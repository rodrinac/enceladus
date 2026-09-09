library(dplyr)
library(stringi)
library(stringr)
library(janitor)
require(rmarkdown)
library(microdatasus)
library(lubridate)
library(matrixStats)

Sys.setlocale("LC_TIME", "pt_BR.UTF-8")

args <- commandArgs(TRUE)

estados <- unlist(strsplit(args[[1]], ","))
data_inicio <- as.Date(args[[2]])
data_fim <- as.Date(args[[3]])
nome_arquivo <- args[[4]]
diretorio_de_trabalho <- args[[5]]


data_inicio <- floor_date(data_inicio, "month")
data_fim <- ceiling_date(data_fim, "month") - days(1)

ano_inicio <- strftime(data_inicio, "%Y")
ano_fim <- strftime(data_fim, "%Y")

source("rscripts/codigos_cid10.R")

dados_estados <- vector(mode = "list", length = length(estados))

regex <- stri_paste(codigos_cid10, collapse = "|")

for (i in seq_len(length(estados))) {
  dados <- fetch_datasus(
    year_start = ano_inicio,
    year_end = ano_fim,
    uf = estados[i],
    information_system = "SIM-DO"
  )

  dados_filtrados <- filter(
    dados,
    str_detect(CAUSABAS, regex) | str_detect(CAUSABAS_O, regex)
  )

  dados_filtrados[["ESTADO"]] <- estados[i]


  dados_estados[[i]] <- dados_filtrados
}

dados <- bind_rows(dados_estados)


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

dados_processados <- subset(
  dados_processados,
  as.Date(dtobito) > data_inicio &
    as.Date(dtobito) < data_fim &
    substr(codmunres, 3, 6) != "0000"
)

# soma o total de casos em cada municipio
dados_finais <- setNames(
  aggregate(
    x = as.integer(dados_processados$origem), # Base para contador (agregando)
    by = list(dados_processados$estado), # Identifica o que está duplicado
    FUN = sum
  ),
  c("estado", "total")
) # Nomeia as colunas


meses <- seq(as.Date(data_inicio), as.Date(data_fim), by = "months")
meses <- strftime(meses, "%b/%Y", origin = lubridate::origin)

for (mes in meses) {
  dados_finais[[mes]] <- 0
}

for (i in seq_len(nrow(dados_finais))) {
  row <- dados_finais[i, ]

  for (mes in meses) {
    dados_finais[dados_finais$estado == row$estado, mes] <- nrow(
      dados_processados %>%
        filter(estado == row$estado &
          strftime(dtobito, "%b/%Y") == mes)
    )
  }
}

dados_finais$sd <- rowSds(as.matrix(dados_finais[, meses]))

desvio_padrao_por_estado <- dados_finais[, c("estado", meses, "total", "sd")]

colnames(desvio_padrao_por_estado)[
  colnames(desvio_padrao_por_estado) == "sd"] <- "Desvio Padrão"


# soma o total de casos em cada municipio
municipios_com_casos <- dados_processados %>%
  group_by(across(all_of(c("codmunres", "mun_res_nome", "estado")))) %>%
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

municipios_com_casos <- municipios_com_casos %>%
  arrange(desc(densidade)) %>%
  group_by(estado) %>%
  slice(1:5)

municipios_com_casos <- municipios_com_casos[
  ,
  c("mun_res_nome", "estado", "populacao", "n", "densidade")
]

municipios_com_casos$mun_res_nome <- str_trunc(
  municipios_com_casos$mun_res_nome,
  30
)

write.csv(
  desvio_padrao_por_estado,
  paste0(diretorio_de_trabalho, "desvio_padrao_por_estado.csv", sep = ""),
  row.names = FALSE
)


write.csv(
  municipios_com_casos,
  paste0(diretorio_de_trabalho, "municipios_com_casos.csv", sep = ""),
  row.names = FALSE
)


rmarkdown::render("rscripts/densidade_municipal_por_periodo_geral.Rmd",
  output_file = nome_arquivo,
  params = list(
    diretorio = diretorio_de_trabalho,
    dt_inicio = data_inicio,
    dt_fim = data_fim,
    uf = stri_paste(estados, collapse = ", ")
  )
)
