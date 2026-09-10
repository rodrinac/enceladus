source("rscripts/fetch_datasus_cached.R")

cache_dir <- tempfile("datasus-cache-")
Sys.setenv(
  ENCELADUS_DATASUS_CACHE_PATH = cache_dir,
  ENCELADUS_DATASUS_CACHE_MAX_BYTES = "1000000"
)

fetch_count <- 0L
fake_fetch <- function(year_start, year_end, uf, information_system, ...) {
  fetch_count <<- fetch_count + 1L
  data.frame(year = year_start, state = uf)
}

first <- fetch_datasus_cached(2024L, 2024L, "DF", "SIM-DO", fetch = fake_fetch)
second <- fetch_datasus_cached(2024L, 2024L, "DF", "SIM-DO", fetch = fake_fetch)

stopifnot(fetch_count == 1L, identical(first, second))
stopifnot(length(list.files(cache_dir, pattern = "\\.rds$")) == 1L)
cat("PASS persistent DataSUS cache\n")
