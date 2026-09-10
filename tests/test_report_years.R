# Run from src with the application's R dependencies installed.
scripts <- c(
  "densidade_municipal_por_periodo",
  "densidade_municipal_por_periodo_geral",
  "casos_mensais_por_municipio_por_estado"
)

for (script in scripts) {
  monthly <- script == "casos_mensais_por_municipio_por_estado"
  for (year in c("2019", "2024")) {
    environment <- new.env(parent = globalenv())
    environment$commandArgs <- function(...) c(
      "DF",
      if (monthly) year else paste0(year, "-01-01"),
      if (monthly) year else paste0(year, "-12-31"),
      tempfile(fileext = ".pdf"),
      tempdir()
    )
    environment$fetch_datasus <- function(year_start, year_end, ...) {
      stopifnot(
        is.integer(year_start), length(year_start) == 1L,
        is.integer(year_end), length(year_end) == 1L,
        year_start == as.integer(year), year_end == as.integer(year)
      )
      stop(structure(list(message = "validated"), class = c("validated_years", "error", "condition")))
    }
    validated <- tryCatch({
      eval(parse(file = file.path("rscripts", paste0(script, ".R"))), environment)
      FALSE
    }, validated_years = function(error) TRUE)
    stopifnot(validated)
    cat("PASS", script, year, "\n")
  }
}
