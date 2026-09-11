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

Antes de iniciar a API, o aplicativo `population` do flake Nix executa `scripts/fetch_population.py`.

1. Consulta a tabela SIDRA 6579 para obter a estimativa populacional dos municípios.
2. Valida formato, população positiva, unicidade dos códigos e uma quantidade mínima de 5.500 municípios.
3. Grava `population.csv` atomicamente: primeiro em arquivo temporário e depois por renomeação.
4. Consulta o catálogo do OpenDataSUS para descobrir o maior ano final disponível para o SIM.
5. Limita esse ano a 2024 e grava `datasus-max-year.txt`, também atomicamente.
6. Se o SIDRA falhar depois de três tentativas, preserva um `population.csv` anterior somente se ele ainda passar pela validação. Sem dados válidos, a inicialização falha.
7. Se a descoberta do ano falhar, mantém o valor previamente salvo ou usa `DATASUS_MAX_YEAR_FALLBACK`, atualmente 2024.

Em produção, a unit `enceladus-population` do systemd grava os arquivos em `/srv/enceladus/population` no volume EBS. A aplicação lê os caminhos pelas variáveis `ENCELADUS_POPULATION_DATA_PATH` e `ENCELADUS_DATASUS_MAX_YEAR_PATH`; os padrões estão em `src/settings.py`.

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
3. registra `queued` no registro de status em memória (`src/job_status.py`);
4. agenda a função Python do relatório via `_processar_relatorio`, que marca `running` ao iniciar e `succeeded` ou `failed` no desfecho;
5. devolve imediatamente HTTP `202`, destino e código da requisição.

O registro de status é intencionalmente em memória: um restart do processo o esvazia. Não existe fila durável nem endpoint de estado por `id_requisicao`; o identificador também serve para correlacionar logs e nomear o diretório temporário. Reiniciar o processo ou o contêiner pode interromper tarefas em andamento e apagar os status pendentes.

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
4. caso contrário, inicia o script R pelo `run_rscript` compartilhado, captura stdout e stderr juntos e espera sua conclusão;
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

Pandoc, LaTeX e os pacotes R necessários são instalados pelo flake Nix (`flake.nix`). O arquivo final é gravado diretamente no diretório persistente do tipo de relatório. Falhas de download, parsing, transformação ou renderização propagam um código não zero ao wrapper Python e aparecem no log capturado do Rscript.

## 10. Persistência, listagem e download

Os PDFs ficam em subdiretórios de `settings.reports_dir`, que em produção aponta para `/srv/enceladus/relatorios`. Esse diretório está em um volume EBS separado e retido pela infraestrutura.

Após uma geração nova, `src/storage.py` grava no Redis:

```text
dataProcessamento.<id-do-relatorio>.<nome-do-pdf> = DD/MM/AAAA HH:MM:SS
```

`GET /relatorios/processados` usa `src/relatorios/relatorios.py` para varrer os PDFs, reconstruir estado e período a partir do nome e juntar a data armazenada no Redis. A lista também mescla os trabalhos pendentes do registro em memória (`queued`, `running`, `failed`), exercendo a mesma ordenação por data. A interface consulta essa rota periodicamente. Os endpoints `GET` específicos leem o PDF do disco e o devolvem com `Content-Type: application/pdf`.

Redis usa AOF e volume persistente. A implementação atual presume que todo PDF encontrado possui uma data válida no Redis; um arquivo órfão pode causar erro durante a ordenação da lista.

## 11. Envio por e-mail

`src/send_email.py` monta uma mensagem MIME com texto, HTML e PDF anexado e usa `boto3` para `ses:SendRawEmail` na região `eu-west-1`. O cliente SES é reutilizado em memória. Remetente e configuration set vêm de `SES_SENDER` e `SES_CONFIGURATION_SET`.

O perfil IAM da EC2 autoriza envio pelo SES. Erros de SES ocorrem depois que PDF e metadado já foram persistidos; portanto, o relatório pode aparecer na listagem mesmo quando o destinatário não recebe o e-mail. Não há retentativa ou fila de reenvio na aplicação.

## 12. Deploy

Há dois fluxos independentes:

- `.github/workflows/pages.yml` compila o Next.js como site estático e publica `web/out` no GitHub Pages;
- `.github/workflows/deploy-api.yml` usa credenciais OIDC e Systems Manager para que a EC2 reconstrua o flake Nix e reinicie as units do systemd, sem SSH.

`deploy/deploy.sh` atualiza o checkout em `/opt/enceladus`, obtém o segredo Redis do Secrets Manager, compila os aplicativos `api` e `population` com Nix e reinicia as units `enceladus-api` e `enceladus-population`. Se `/health` não responder, restaura o build anterior pelos out-links `current-*.previous`. O volume EBS preserva PDFs, cache OpenDataSUS, população e Redis através de novos deploys e substituições da instância.

## 13. Estados observáveis e pontos de falha

| Etapa | Sinal atual | Comportamento em falha |
| --- | --- | --- |
| Configuração do navegador | Mensagem na interface | Formulário fica indisponível |
| Aceite do pedido | HTTP `202` + `id_requisicao` | Erro HTTP aparece na interface |
| Processamento assíncrono | Status em memória + logs pelo código | Badge “Falhou” na UI; restart limpa os status |
| OpenDataSUS | Log capturado do R/Python | Rscript termina com erro; sem e-mail |
| Cache concorrente | RDS ou diretório `.lock` | Espera até 300 s; lock velho é removido |
| Renderização | Código de saída do Rscript | PDF não é registrado nem enviado |
| Persistência | PDF no volume + chave Redis | Listagem depende da consistência entre ambos |
| SES | MessageId no log | PDF continua disponível, sem retentativa de e-mail |

Para investigar uma requisição específica, o primeiro ponto de correlação é o `id_requisicao` nos logs da unit `enceladus-api`. O endpoint `/health` confirma somente que o processo HTTP responde; ele não testa OpenDataSUS, SIDRA, Redis, renderização R ou SES.

## 14. Evoluções recomendadas

### Integração entre Python e R

Executar o R em um processo separado continua sendo a opção mais segura para esta aplicação. Os relatórios são pesados, usam bibliotecas R com estado próprio e já rodam fora da thread do servidor; manter essa fronteira também impede que uma falha do runtime R derrube o processo HTTP.

A melhoria imediata é concentrar as três implementações duplicadas em um único `ReportRunner` Python. A implementação usa `subprocess.run(..., check=False, text=True, stdout=PIPE, stderr=STDOUT, timeout=...)`, registra a saída com o código da requisição e padroniza timeout e código de erro. Isso deixa a chamada mais declarativa sem alterar a arquitetura ou o isolamento entre runtimes.

Alternativas avaliadas:

- **rpy2:** incorpora o R ao processo Python e permite chamar funções R diretamente. A interface parece mais elegante, mas acopla instalação, memória, estado e falhas dos dois runtimes. Também exige revisar cuidadosamente concorrência e ciclo de vida do R embutido. Não é a melhor troca para trabalhos longos disparados por um servidor assíncrono.
- **Plumber:** transforma as rotinas R em uma API HTTP separada. É uma boa fronteira se o processamento R precisar escalar e ser implantado independentemente, mas hoje acrescentaria outro serviço, autenticação interna, observabilidade, health checks e retentativas sem eliminar a necessidade de uma fila.
- **Fila com worker dedicado:** é a evolução recomendada quando confiabilidade for prioridade. A API publica o trabalho em SQS (ou em uma fila Redis apropriada), e um worker executa o mesmo comando R isolado. Isso remove a dependência do executor em memória do Quart e permite retentativas e controle explícito de concorrência.

Portanto, a recomendação incremental é primeiro criar o `ReportRunner` compartilhado, depois separar o worker por meio de uma fila. Trocar `subprocess` por uma ponte embutida não resolve a principal fragilidade operacional.

### Status de processamento na interface

Uma versão mínima em memória já está implementada em `src/job_status.py`: a rota `POST` registra `queued` antes do `202`, o executor marca `running` ao começar e, no desfecho, `succeeded` (removendo o trabalho, pois o PDF passa a ser a fonte de verdade) ou `failed` com uma mensagem pública. `GET /relatorios/processados` mescla esses trabalhos com os PDFs concluídos, e a UI exibe badges “Na fila”, “Processando”, “Concluído” e “Falhou”, mostrando o botão de download apenas em `succeeded`.

Para tornar o status durável, é possível expô-lo usando o Redis já existente, sem esperar pela migração para uma fila. Cada requisição teria uma chave própria, por exemplo `processamento.<id_requisicao>`, com TTL e os campos:

```text
id, tipo, estados, data_inicio, data_fim
status = queued | running | succeeded | failed
criado_em, iniciado_em, finalizado_em
uri (somente quando concluído)
mensagem (erro público genérico, sem e-mail ou stack trace)
```

O fluxo mínimo seria:

1. a rota `POST` grava `queued` antes de responder `202`;
2. o executor grava `running` ao começar;
3. ao publicar o PDF, grava `succeeded` e a URI;
4. qualquer exceção grava `failed`, enquanto o detalhe técnico permanece apenas nos logs;
5. `GET /relatorios` lista trabalhos recentes, inclusive em andamento, e `GET /relatorios/<id>` permite consultar um pedido específico;
6. a UI faz polling enquanto houver itens `queued` ou `running` e exibe badges “Na fila”, “Processando”, “Concluído” e “Falhou”; o botão de download aparece apenas em `succeeded`.

A interface pode guardar no `localStorage` os IDs criados naquele navegador para acompanhar imediatamente um pedido. A listagem geral deve vir da API, pois depender apenas do navegador esconderia trabalhos feitos por outros dispositivos.

Esse status melhora visibilidade, mas não torna o processamento durável: hoje um restart do contêiner pode perder uma tarefa que está apenas no `run_in_executor`. Para garantir execução, o status deve evoluir junto com SQS e um worker. A tabela pode então ser alimentada diretamente pelos estados da fila, preservando o mesmo contrato HTTP e os mesmos badges da UI.
