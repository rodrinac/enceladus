library(dplyr)
library(stringi)
library(stringr)
library(janitor)
require(rmarkdown)
library(microdatasus)


args <- commandArgs(TRUE)

estado <- args[[1]]
data_inicio <- as.Date(args[[2]])
data_fim <- as.Date(args[[3]])
nome_arquivo <- args[[4]]
diretorio_de_trabalho <- args[[5]]


ano_inicio <- as.integer(strftime(data_inicio, "%Y"))
ano_fim <- as.integer(strftime(data_fim, "%Y"))

source("rscripts/codigos_cid10.R")
source("rscripts/fetch_datasus_cached.R")

dados <- fetch_datasus_cached(
  year_start = ano_inicio,
  year_end = ano_fim,
  uf = estado,
  information_system = "SIM-DO"
)

filtrar_por_causas <- function(df, codigos) {
  regex <- stringi::stri_paste(codigos, collapse = "|")

  big_data <- subset(
    df,
    str_detect(CAUSABAS, regex) | str_detect(CAUSABAS_O, regex)
  )

  return(big_data)
}

dados_filtrados <- filtrar_por_causas(dados, codigos_cid10)

dados_processados <- process_sim(dados_filtrados) %>%
  janitor::clean_names()

dados_processados <- subset(dados_processados,
  as.Date(dtobito) > data_inicio
    & as.Date(dtobito) < data_fim
    & substr(codmunres, 3, 6) != "0000"
)

contar_por_causas <- function(df, codigos) {
  regex <- stringi::stri_paste(codigos, collapse = "|")

  big_data <- subset(
    df,
    str_detect(causabas, regex) | str_detect(causabas_o, regex)
  )

  return(nrow(big_data))
}

contagem_por_tipo <- data.frame(
  tipo = c("eletrica", "clr_e_term", "quimica", "gelad_e_rad"),
  quantidade = c(
    contar_por_causas(dados_processados, codigos_causa_eletrica),
    contar_por_causas(dados_processados, codigos_causa_clr_e_term),
    contar_por_causas(dados_processados, codigos_causa_quimica),
    contar_por_causas(dados_processados, codigos_causa_gelad_e_rad)
  )
)

## DIAGRAMA DE LOCAL

## TABELA DE CASOS POR CIDADE/MUNICIPIO

# soma o total de casos em cada municipio
municipios_com_casos <- dados_processados %>%
  group_by(across(all_of(c("codmunres", "mun_res_nome")))) %>%
  tally() # Now summarise with unique elements

population_data_path <- Sys.getenv(
  "ENCELADUS_POPULATION_DATA_PATH",
  "./data/static/population.csv"
)

if (!file.exists(population_data_path)) {
  stop(paste("Population data file not found:", population_data_path))
}

projecao_populacional <- read.csv(population_data_path)

projecao_populacional$city_ibge_code <- as.character(
  substr(projecao_populacional$city_ibge_code, 1, 6)
)

municipios_com_casos$populacao <- NA
municipios_com_casos$densidade <- NA

municipios_com_casos <- as.data.frame(municipios_com_casos)

for (i in rownames(municipios_com_casos)) {
  codigo_municipio <- municipios_com_casos[[i, "codmunres"]]

  projecao <- projecao_populacional[
    projecao_populacional$city_ibge_code == codigo_municipio,
    "estimated_population"
  ]

  municipios_com_casos[[i, "populacao"]] <- projecao
  municipios_com_casos[[i, "densidade"]] <- (
    municipios_com_casos[[i, "n"]] / projecao) * 100000
}

municipios_sem_casos <- projecao_populacional[
  !(projecao_populacional$city_ibge_code %in% municipios_com_casos$codmunres),
, ]

municipios_sem_casos <- municipios_sem_casos[
  municipios_sem_casos$state == estado,
  c("city", "estimated_population")
]


nrows_mun_sem_casos <- nrow(municipios_sem_casos)

if (nrows_mun_sem_casos > 0) {
  rownames(municipios_sem_casos) <- seq.int(nrows_mun_sem_casos)
}

municipios_com_casos <- municipios_com_casos[,
  c("mun_res_nome", "populacao", "n", "densidade")
]

municipios_com_casos$mun_res_nome <- str_trunc(
  municipios_com_casos$mun_res_nome,
  30
)

write.csv(
  municipios_com_casos,
  paste0(diretorio_de_trabalho, "municipios_com_casos.csv", sep = ""),
  row.names = FALSE
)

write.csv(
  municipios_sem_casos,
  paste0(diretorio_de_trabalho, "municipios_sem_casos.csv", sep = ""),
  row.names = FALSE
)

write.csv(
  contagem_por_tipo,
  paste0(diretorio_de_trabalho, "contagem_por_tipo.csv", sep = ""),
  row.names = FALSE
)

rmarkdown::render("rscripts/densidade_municipal_por_periodo.Rmd",
  output_file = nome_arquivo,
  params = list(
    diretorio = diretorio_de_trabalho,
    dt_inicio = data_inicio,
    dt_fim = data_fim,
    uf = estado
  )
)
