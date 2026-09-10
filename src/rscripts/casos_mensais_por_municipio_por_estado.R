library(microdatasus)
library(dplyr)
library(stringi)
library(stringr)
library(gridExtra)
library(grid)
library(lubridate)

args <- commandArgs(TRUE)

estados <- strsplit(args[[1]], ",")
ano_inicio <- as.integer(args[[2]])
ano_fim <- as.integer(args[[3]])
nome_arquivo <- args[[4]]
diretorio_de_trabalho <- args[[5]]

source("rscripts/codigos_cid10.R")
source("rscripts/fetch_datasus_cached.R")

regex <- stri_paste(codigos_cid10, collapse = "|")

datalist <- list()

for (i in seq_len(length(estados))) {
  dados <- fetch_datasus_cached(
    year_start = ano_inicio,
    year_end = ano_fim,
    uf = estados[[i]],
    information_system = "SIM-DO"
  )

  dados_filtrados <- filter(
    dados,
    str_detect(CAUSABAS, regex) | str_detect(CAUSABAS_O, regex)
  )

  datalist[[i]] <- dados_filtrados
}


big_data <- bind_rows(datalist)

dados_processados <- process_sim(big_data)

## DIAGRAMA DE LOCAL

ocorrencias <- as.data.frame(t(table(dados_processados$LOCOCOR)))

diagrama_ocorrencias <- data.frame(
  local = ocorrencias$Var2,
  ocorrencias = ocorrencias$Freq,
  colors = rainbow(length(ocorrencias$Freq))
)

## TABELA DE CASOS POR CIDADE/MUNICIPIO

# Isola os nomes de municipios
municipios <- unique(dados_processados$munResNome)

# soma o total de casos em cada municipio
dados_finais <- setNames(
  aggregate(
    x = as.integer(dados_processados$ORIGEM), # Base para contador (agregando)
    by = list(dados_processados$munResNome), # Identifica o que está duplicado
    FUN = sum
  ),
  c("Municipio", "Total")
) # Nomeia as colunas

meses <- c(
  "Jan", "Fev", "Mar", "Abr",
  "Mai", "Jun", "Jul", "Ago",
  "Set", "Out", "Nov", "Dez"
)

for (mes in meses) {
  dados_finais[[mes]] <- vector(length = nrow(dados_finais))
}


for (i in seq_len(nrow(dados_finais))) {
  municipio <- dados_finais[i, ]

  regex <- "\\d{4}-01-\\d{2}"

  for (i in 1:12) {
    mes <- meses[[i]]


    dados_finais[dados_finais$Municipio == municipio$Municipio, mes] <- nrow(
      dados_processados %>%
        filter(munResNome == municipio$Municipio &
          as.numeric(strftime(DTOBITO, "%m")) == i)
    )
  }
}

# Cria header com o nome das colunas
dados_finais <- dados_finais[, c("Municipio", meses, "Total")]

dados_finais$Municipio <- str_trunc(
  dados_finais$Municipio,
  30
)


write.csv(
  diagrama_ocorrencias,
  paste0(
    diretorio_de_trabalho,
    "diagrama_ocorrencias.csv",
    sep = ""
  ),
  row.names = FALSE
)

write.csv(
  dados_finais,
  paste0(
    diretorio_de_trabalho,
    "casos_mensais_por_municipio_no_estado.csv",
    sep = ""
  ),
  row.names = FALSE
)

rmarkdown::render("rscripts/casos_mensais_por_municipio_por_estado.Rmd",
  output_file = nome_arquivo,
  params = list(
    diretorio = diretorio_de_trabalho,
    ano_fim = ano_fim,
    ano_inicio = ano_inicio,
    uf = args[[1]]
  )
)
