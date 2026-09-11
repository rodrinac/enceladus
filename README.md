# Enceladus Big Data

## O que é?

O Enceladus Big Data ajuda pesquisadores da Sociedade Brasileira de Queimaduras (SBQ) a
coletar, processar e transformar dados brutos em relatórios úteis. O stack combina uma
interface Next.js, uma API Quart e processamento estatístico em R.

Desenvolvido por [José Inácio Rodrigues da Silva](https://github.com/josersinacio) e
[Josué de Paulo Viana](https://github.com/josuepviana), com orientação do
[Me. Fábio Ferraz Fernandez](http://lattes.cnpq.br/9386664812059696) e colaboração do
[Dr. Sérgio Eduardo Soares Fernandes](http://lattes.cnpq.br/9797758799188189).

## Executar localmente

O [flake Nix](flake.nix) fornece um ambiente reprodutível com Python 3.12, R (com
`microdatasus` e as dependências dos relatórios), Pandoc, LaTeX e os aplicativos da API
(`enceladus-api`) e da população IBGE (`enceladus-population`):

    nix develop            # shell interativo com todas as ferramentas
    nix run .#api          # inicia o Hypercorn com a API
    nix run .#population   # busca população IBGE e o ano máximo de SIM

A API espera um Redis em `localhost:6379` por padrão (`REDIS_HOST`/`REDIS_PORT`/
`REDIS_DB` são configuráveis). Fora do Linux, suba o Redis com o gerenciador da sua
máquina; no Linux, o dev shell já inclui o `redis` do nixpkgs. Antes de usar os
relatórios, execute `nix run .#population` uma vez para gerar o denominador de
densidade e o limite de anos do DataSUS.

Para ajustar o endereço do Hypercorn:

    ENCELADUS_BIND=0.0.0.0:8000 nix run .#api

Os arquivos de saída ficam em `ENCELADUS_HOME` (padrão `$PWD/.enceladus`): população em
`data/`, relatórios em `relatorios/` e o cache do DataSUS em `relatorios/.cache/`.

A interface em `web/` roda com Node.js:

    cd web
    npm install
    NEXT_PUBLIC_API_URL=http://localhost:8000 npm run dev

## População municipal do IBGE

Antes de iniciar a API, o aplicativo `population` consulta a estimativa municipal mais
recente na API SIDRA oficial do IBGE, usando a tabela 6579, variável 9324. O CSV validado
é escrito de forma atômica em `ENCELADUS_POPULATION_DATA_PATH` (padrão
`$ENCELADUS_HOME/data/population.csv`) e lido pela API em modo somente leitura.

Se o IBGE estiver temporariamente indisponível, uma cópia válida já armazenada será
reutilizada. A primeira inicialização exige acesso ao SIDRA. Para fixar um ano
reprodutível, defina `IBGE_POPULATION_PERIOD`, por exemplo `2025`.

Para atualizar os dados manualmente:

    nix run .#population

Essas estimativas anuais são usadas como denominador dos relatórios de densidade; não são
os resultados do Censo 2022. Consulte a
[tabela 6579 do SIDRA](https://sidra.ibge.gov.br/tabela/6579) e a
[página oficial das estimativas](https://www.ibge.gov.br/estatisticas/sociais/populacao/9103-estimativas-de-populacao.html).

Na mesma execução, o aplicativo consulta os arquivos finais de mortalidade disponíveis
e grava o último ano publicado. A interface usa esse valor como limite; se a descoberta
estiver indisponível, mantém o último valor válido ou usa 2024 na primeira execução.

Os dados SIM são baixados dos arquivos anuais oficiais do OpenDataSUS sobre HTTPS e
filtrados por estado durante a leitura. Os arquivos nacionais e resultados filtrados ficam
em `ENCELADUS_DATASUS_CACHE_PATH`; pedidos repetidos reutilizam o cache. Escritas são
atômicas, pedidos concorrentes compartilham o mesmo download e os resultados mais antigos
são removidos quando o cache ultrapassa 5 GiB. Defina `ENCELADUS_DATASUS_CACHE_MAX_BYTES`
para alterar esse limite.

## Interface e GitHub Pages

A interface em `web/` usa Next.js, TypeScript, Tailwind CSS e o tema Catppuccin Latte. A
exportação estática é publicada no GitHub Pages; Python, R, Pandoc e LaTeX não precisam
ser instalados no host.

O workflow `.github/workflows/pages.yml` publica a exportação estática quando há mudanças
em `web/` na branch `main`. Antes da primeira publicação:

1. Configure a fonte do GitHub Pages como **GitHub Actions**.
2. Crie a variável de repositório `NEXT_PUBLIC_API_URL` com a URL HTTPS pública da API.
3. Configure `CORS_ORIGINS` na API com a origem do GitHub Pages, por exemplo
   `https://usuario.github.io`.

O build detecta automaticamente o nome do repositório e configura o `basePath` dos assets.

## Produção

A API é publicada em uma única instância EC2 na região `eu-west-1`, executando os
aplicativos do flake Nix diretamente no host por meio de units do systemd. O pipeline
usa GitHub OIDC e Systems Manager, sem chaves AWS persistentes, registro de contêineres
ou acesso SSH. API Gateway fornece o endereço HTTPS, uma função Lambda encaminha as
chamadas pela VPC e o volume EBS criptografado preserva relatórios, Redis e o cache do
IBGE.

Consulte o [guia de implantação](deploy/README.md) para provisionar a infraestrutura,
configurar DNS, preparar os ambientes do GitHub e executar o primeiro release.

## Desenvolvimento e validação

Após alterar a API, os scripts R ou a interface, reconstrua e valide o stack completo:

    nix build .#api
    pytest
    ruff check .

As configurações são controladas por variáveis de ambiente (Redis, SES, CORS, períodos
do IBGE e caminhos de dados); consulte `src/settings.py` e `.env.example` como
referência.

Para verificar cada dependência externa sem gerar relatórios ou enviar e-mail:

    python scripts/probe_dependencies.py \
      --api-url https://sua-api.execute-api.eu-west-1.amazonaws.com

O comando testa DNS, a tabela 6579 do SIDRA, listagem e download parcial dos arquivos
SIM-DO no FTP do DataSUS e os endpoints públicos da API. Use `--check-ses` para acrescentar
uma consulta somente leitura à conta SES configurada, ou `--json` para saída estruturada.
O processo retorna código diferente de zero quando qualquer verificação executada falha.
Em redes com inspeção TLS, passe o certificado corporativo em PEM com `--ca-bundle`.
`--insecure` serve apenas para confirmar se uma falha vem da cadeia de certificados local.
O diagnóstico também compara o FTP com a API e o arquivo ZIP anual publicados pelo
OpenDataSUS sobre HTTPS.