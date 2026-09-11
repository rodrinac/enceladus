# Pipeline de dados e geração de relatórios

Este documento descreve o fluxo atual do Enceladus, desde a abertura da interface até a publicação e o envio de um relatório. Os caminhos citados são relativos à raiz do repositório.

## Visão geral

```text
GitHub Pages (Next.js estático)
  -> API Gateway HTTP API
  -> Lambda proxy
  -> Quart/Hypercorn na EC2
  -> executor em thread
  -> Rscript
     -> Python: arquivos anuais SIM no OpenDataSUS/S3
     -> R: filtro CID-10 + microdatasus::process_sim
     -> CSVs intermediários + R Markdown
  -> PDF em volume persistente
  -> metadado no Redis
  -> anexo enviado pelo Amazon SES
```

O aceite HTTP e o processamento são separados: uma resposta `202` informa apenas que a tarefa foi entregue ao executor local, não que o PDF foi concluído.

## 1. Preparação dos dados de referência

Antes de iniciar a API, o serviço `population-data` executa `docker/ibge/fetch_population.py`.

1. Consulta a tabela SIDRA 6579 para obter a estimativa populacional dos municípios.
2. Valida formato, população positiva, unicidade dos códigos e uma quantidade mínima de 5.500 municípios.
3. Grava `population.csv` atomicamente: primeiro em arquivo temporário e depois por renomeação.
4. Consulta o catálogo do OpenDataSUS para descobrir o maior ano final disponível para o SIM.
5. Limita esse ano a 2024 e grava `datasus-max-year.txt`, também atomicamente.
6. Se o SIDRA falhar depois de três tentativas, preserva um `population.csv` anterior somente se ele ainda passar pela validação. Sem dados válidos, a inicialização falha.
7. Se a descoberta do ano falhar, mantém o valor previamente salvo ou usa `DATASUS_MAX_YEAR_FALLBACK`, atualmente 2024.

Em produção, `deploy/compose.yml` monta `/srv/enceladus/population` no contêiner da aplicação como somente leitura. A aplicação lê os caminhos pelas variáveis `ENCELADUS_POPULATION_DATA_PATH` e `ENCELADUS_DATASUS_MAX_YEAR_PATH`; os padrões estão em `src/settings.py`.

## 2. Carregamento da interface

A página em `web/app/page.tsx` é exportada estaticamente e publicada no GitHub Pages. O endereço da API é incorporado no build por `NEXT_PUBLIC_API_URL`, usado por `web/lib/api.ts`.

Ao abrir a página, o navegador faz em paralelo:

- `GET /config/anos`: intervalo configurado em `src/config.yml`, com o último ano substituído pelo ano descoberto quando o arquivo for válido;
- `GET /config/relatorios`: tipos, caminhos e capacidade de selecionar múltiplos estados;
- `GET /config/estados`: lista das unidades federativas;
- `GET /relatorios/processados`: PDFs já disponíveis.

O formulário começa no período de um ano anterior ao ano-limite até o fim do próprio ano-limite. A lista de relatórios é atualizada a cada dez segundos e também pelo botão **Atualizar**.

## 3. Submissão de um relatório

`web/lib/api.ts` transforma o formulário em um `POST` com parâmetros na query string:

- relatórios de densidade recebem `estado`, `data_inicio`, `data_fim` e `email`;
- casos mensais recebem `estado`, `ano_inicio`, `ano_fim` e `email`;
- relatórios multiestado repetem o parâmetro `estado`.

As rotas correspondentes ficam em `src/main.py`. A API:

1. lê os parâmetros;
2. cria `id_requisicao` com `uuid.uuid1()`;
3. agenda a função Python do relatório com `asyncio.get_event_loop().run_in_executor(None, ...)`;
4. devolve imediatamente HTTP `202`, destino e código da requisição.

Não existe atualmente uma fila durável nem endpoint de estado por `id_requisicao`. O identificador serve para correlacionar logs e nomear o diretório temporário. Reiniciar o processo ou o contêiner pode interromper tarefas em andamento.

## 4. Entrada AWS e execução da API

Em produção, o tráfego segue a infraestrutura definida em `infra/ec2.yml`:

1. o API Gateway HTTP API fornece HTTPS e CORS;
2. uma Lambda dentro da VPC preserva método, caminho, query string e corpo e encaminha a chamada para a porta 8000 do IP privado da EC2;
3. o security group da EC2 aceita a porta 8000 apenas do security group da Lambda;
4. o Hypercorn executa a aplicação Quart do `src/main.py` no contêiner `app`.

A Lambda tem timeout de 29 segundos, mas isso não limita a geração porque a rota devolve `202` antes do trabalho pesado. O processamento continua dentro do processo Hypercorn em uma thread do executor padrão.

## 5. Wrapper Python de cada relatório

Os três wrappers ficam em `src/relatorios/`:

- `densidade_municipal_por_periodo.py`;
- `densidade_municipal_por_periodo_geral.py`;
- `casos_mensais_por_municipio_por_estado.py`.

Cada wrapper:

1. calcula o nome estável do PDF a partir do tipo, estados e período;
2. cria um diretório temporário com o `id_requisicao`;
3. reutiliza o PDF se já existir;
4. caso contrário, inicia o script R com `subprocess.Popen`, captura stdout e stderr juntos e espera sua conclusão;
5. se o R terminar com código diferente de zero, registra o erro e encerra sem salvar metadado nem enviar e-mail;
6. em caso de sucesso, grava no Redis a data de processamento;
7. abre o PDF e chama `src/send_email.py`.

Nos relatórios gerais, o valor especial `TODOS` é expandido para todas as UFs. Os diretórios temporários dos dois relatórios de densidade são removidos após o processamento; o wrapper de casos mensais não faz essa remoção atualmente.

O cache por PDF evita recalcular uma combinação já pronta. Entretanto, não há lock no arquivo final: duas requisições simultâneas com os mesmos parâmetros podem tentar gerar o mesmo PDF. Os diretórios intermediários são distintos, mas o destino final é compartilhado.

## 6. Download dos arquivos anuais do SIM

Os scripts R carregam `src/rscripts/fetch_datasus_cached.R`, que delega a extração a `src/scripts/fetch_sim_archives.py`.

O Python mantém um mapa explícito de arquivos oficiais anuais:

- 2019–2021 e 2023 usam ZIPs CSV `Mortalidade_Geral_<ano>_csv.zip`;
- 2022 usa o ZIP JSON, porque o recurso CSV correspondente não está acessível;
- 2024 usa `DO24OPEN_csv.zip`.

Para cada ano solicitado:

1. procura o arquivo em `<cache>/archives/sim-<ano>.zip`;
2. se não existir ou estiver vazio, baixa por HTTPS com timeout de 120 segundos;
3. grava primeiro um arquivo `.part` e publica por renomeação, evitando tratar download parcial como válido;
4. lê o CSV separado por ponto e vírgula ou percorre o array JSON incrementalmente, sem carregar o arquivo JSON inteiro em memória;
5. valida a existência de `CODMUNRES`;
6. mantém somente registros cujo prefixo de município corresponde às UFs solicitadas;
7. unifica as colunas encontradas entre anos e escreve um CSV temporário para o R.

O mapa cobre apenas 2019–2024. Uma solicitação fora dele termina com erro explícito, mesmo que `src/config.yml` ainda contenha anos anteriores.

## 7. Cache de dados filtrados e concorrência

Além dos ZIPs anuais persistentes, `fetch_datasus_cached.R` mantém um cache RDS por combinação de versão, sistema, período e UFs. O caminho padrão de produção é `/var/lib/enceladus/relatorios/.cache/datasus`.

Em um cache miss:

1. tenta criar `<chave>.rds.lock` atomicamente;
2. remove locks com mais de dez minutos, considerados abandonados;
3. quem não obtém o lock espera até cinco minutos pelo RDS produzido por outra execução;
4. o processo dono do lock chama o extrator Python, com timeout de 900 segundos;
5. salva o data frame em RDS gzip temporário e o renomeia para o nome final;
6. remove o lock ao sair;
7. elimina caches RDS mais antigos até respeitar `ENCELADUS_DATASUS_CACHE_MAX_BYTES`, padrão de 5 GiB.

Um RDS ilegível é removido e reconstruído. Os ZIPs em `archives/` não entram no cálculo de poda de RDS. Também não há lock específico no download de um ZIP anual; duas chaves RDS concorrentes que precisem do mesmo ano podem iniciar downloads simultâneos para arquivos `.part` diferentes e publicar no mesmo destino.

## 8. Transformação epidemiológica em R

Os códigos de queimaduras e exposições são definidos em `src/rscripts/codigos_cid10.R`, derivados da configuração CID-10 do projeto.

Os três scripts filtram os registros brutos quando `CAUSABAS` ou `CAUSABAS_O` corresponde aos códigos selecionados. Em seguida, `microdatasus::process_sim` converte e enriquece campos do SIM, incluindo datas e nomes de municípios.

### Densidade municipal por período

`src/rscripts/densidade_municipal_por_periodo.R`:

1. busca uma UF no intervalo anual necessário;
2. filtra CID-10 e processa o SIM;
3. aplica o intervalo exato de datas e descarta códigos municipais terminados em `0000`;
4. conta casos por categoria de causa e por município;
5. associa o município à estimativa em `population.csv` pelos seis primeiros dígitos do código IBGE;
6. calcula `casos / população * 100.000`;
7. produz CSVs de municípios com casos, sem casos e contagem por causa.

### Densidade municipal geral

`src/rscripts/densidade_municipal_por_periodo_geral.R`:

1. arredonda o início para o primeiro dia do mês e o fim para o último;
2. busca e filtra cada UF;
3. une os resultados e processa o SIM;
4. calcula totais mensais e desvio-padrão por estado;
5. calcula densidade por 100 mil habitantes;
6. seleciona os cinco municípios de maior densidade por estado;
7. escreve os CSVs usados pelo template.

### Casos mensais por município e estado

`src/rscripts/casos_mensais_por_municipio_por_estado.R`:

1. busca cada UF e filtra CID-10;
2. une e processa os registros;
3. agrega local de ocorrência do óbito;
4. agrega casos por município, mês e total;
5. escreve os dois CSVs intermediários.

Nos dois scripts de densidade, as comparações de data atuais usam `>` e `<`; portanto, os dias exatamente iguais aos limites não são incluídos. O relatório de casos mensais agrega os meses de todos os anos selecionados nas mesmas doze colunas.

## 9. Renderização do PDF

Cada script R chama `rmarkdown::render` com um template `.Rmd` homônimo em `src/rscripts/`. Os CSVs intermediários ficam no diretório exclusivo da requisição e são passados ao template por `params`.

Pandoc, LaTeX e os pacotes R necessários são instalados no `Dockerfile`. O arquivo final é gravado diretamente no diretório persistente do tipo de relatório. Falhas de download, parsing, transformação ou renderização propagam um código não zero ao wrapper Python e aparecem no log capturado do Rscript.

## 10. Persistência, listagem e download

Os PDFs ficam em subdiretórios de `settings.reports_dir`, montado sobre `/srv/enceladus/reports` em produção. Esse diretório está em um volume EBS separado e retido pela infraestrutura.

Após uma geração nova, `src/storage.py` grava no Redis:

```text
dataProcessamento.<id-do-relatorio>.<nome-do-pdf> = DD/MM/AAAA HH:MM:SS
```

`GET /relatorios/processados` usa `src/relatorios/relatorios.py` para varrer os PDFs, reconstruir estado e período a partir do nome e juntar a data armazenada no Redis. A interface consulta essa rota periodicamente. Os endpoints `GET` específicos leem o PDF do disco e o devolvem com `Content-Type: application/pdf`.

Redis usa AOF e volume persistente. A implementação atual presume que todo PDF encontrado possui uma data válida no Redis; um arquivo órfão pode causar erro durante a ordenação da lista.

## 11. Envio por e-mail

`src/send_email.py` monta uma mensagem MIME com texto, HTML e PDF anexado e usa `boto3` para `ses:SendRawEmail` na região `eu-west-1`. O cliente SES é reutilizado em memória. Remetente e configuration set vêm de `SES_SENDER` e `SES_CONFIGURATION_SET`.

O perfil IAM da EC2 autoriza envio pelo SES. Erros de SES ocorrem depois que PDF e metadado já foram persistidos; portanto, o relatório pode aparecer na listagem mesmo quando o destinatário não recebe o e-mail. Não há retentativa ou fila de reenvio na aplicação.

## 12. Deploy

Há dois fluxos independentes:

- `.github/workflows/pages.yml` compila o Next.js como site estático e publica `web/out` no GitHub Pages;
- `.github/workflows/deploy-api.yml` constrói a imagem, publica no ECR por digest e usa credenciais OIDC e Systems Manager para atualizar a EC2 sem SSH.

`deploy/deploy.sh` baixa a configuração Compose, obtém o segredo Redis do Secrets Manager, faz pull da imagem e sobe os serviços. Se `/health` não responder, restaura a referência anterior da imagem. O volume EBS preserva PDFs, cache OpenDataSUS, população e Redis através de novos deploys e substituições da instância.

## 13. Estados observáveis e pontos de falha

| Etapa | Sinal atual | Comportamento em falha |
| --- | --- | --- |
| Configuração do navegador | Mensagem na interface | Formulário fica indisponível |
| Aceite do pedido | HTTP `202` + `id_requisicao` | Erro HTTP aparece na interface |
| Processamento assíncrono | Logs da aplicação pelo código | Não há consulta de status pela UI |
| OpenDataSUS | Log capturado do R/Python | Rscript termina com erro; sem e-mail |
| Cache concorrente | RDS ou diretório `.lock` | Espera até 300 s; lock velho é removido |
| Renderização | Código de saída do Rscript | PDF não é registrado nem enviado |
| Persistência | PDF no volume + chave Redis | Listagem depende da consistência entre ambos |
| SES | MessageId no log | PDF continua disponível, sem retentativa de e-mail |

Para investigar uma requisição específica, o primeiro ponto de correlação é o `id_requisicao` nos logs do contêiner `app`. O endpoint `/health` confirma somente que o processo HTTP responde; ele não testa OpenDataSUS, SIDRA, Redis, renderização R ou SES.
