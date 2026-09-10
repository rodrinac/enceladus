datasus_cache_path <- function() {
  Sys.getenv(
    "ENCELADUS_DATASUS_CACHE_PATH",
    "/var/lib/enceladus/relatorios/.cache/datasus"
  )
}

datasus_cache_key <- function(year_start, year_end, uf, information_system) {
  states <- paste(sort(as.character(uf)), collapse = "-")
  key <- paste("open-datasus-v1", information_system, year_start, year_end, states, sep = "_")
  gsub("[^A-Za-z0-9_.-]", "-", key)
}

fetch_open_datasus <- function(
    year_start,
    year_end,
    uf,
    information_system,
    stop_on_error = TRUE,
    timeout = 900) {
  if (information_system != "SIM-DO") {
    stop("OpenDataSUS archive loader only supports SIM-DO")
  }

  cache_dir <- datasus_cache_path()
  output_file <- tempfile(pattern = "open-datasus-", fileext = ".csv")
  on.exit(unlink(output_file), add = TRUE)
  states <- paste(as.character(uf), collapse = ",")
  status <- system2(
    "python",
    c(
      "scripts/fetch_sim_archives.py",
      "--year-start", as.character(year_start),
      "--year-end", as.character(year_end),
      "--states", states,
      "--cache-dir", cache_dir,
      "--output", output_file
    ),
    timeout = timeout
  )
  if (status != 0 || !file.exists(output_file)) {
    stop("Unable to fetch SIM records from the official OpenDataSUS archives")
  }

  data <- read.csv(
    output_file,
    sep = ";",
    quote = "\"",
    colClasses = "character",
    check.names = FALSE,
    stringsAsFactors = FALSE
  )
  if (!nrow(data) && stop_on_error) {
    stop("OpenDataSUS returned no data for the requested period and states")
  }
  data
}

read_datasus_cache <- function(path) {
  if (!file.exists(path)) {
    return(NULL)
  }

  tryCatch(
    readRDS(path),
    error = function(error) {
      warning("Ignoring invalid DataSUS cache file: ", conditionMessage(error))
      unlink(path)
      NULL
    }
  )
}

prune_datasus_cache <- function(cache_dir, current_path) {
  max_bytes <- as.numeric(Sys.getenv("ENCELADUS_DATASUS_CACHE_MAX_BYTES", "5368709120"))
  cache_files <- list.files(cache_dir, pattern = "\\.rds$", full.names = TRUE)
  cache_files <- setdiff(cache_files, current_path)
  if (!length(cache_files)) {
    return(invisible(NULL))
  }

  info <- file.info(c(cache_files, current_path))
  total_bytes <- sum(info$size, na.rm = TRUE)
  for (path in cache_files[order(info[cache_files, "mtime"])]) {
    if (total_bytes <= max_bytes) {
      break
    }
    size <- file.info(path)$size
    if (unlink(path) == 0) {
      total_bytes <- total_bytes - size
    }
  }
  invisible(NULL)
}

fetch_datasus_cached <- function(
    year_start,
    year_end,
    uf,
    information_system,
    fetch = fetch_open_datasus) {
  cache_dir <- datasus_cache_path()
  dir.create(cache_dir, recursive = TRUE, showWarnings = FALSE)
  cache_file <- file.path(
    cache_dir,
    paste0(datasus_cache_key(year_start, year_end, uf, information_system), ".rds")
  )

  cached <- read_datasus_cache(cache_file)
  if (!is.null(cached)) {
    message("Using cached DataSUS data: ", basename(cache_file))
    return(cached)
  }

  lock_dir <- paste0(cache_file, ".lock")
  acquired_lock <- dir.create(lock_dir, showWarnings = FALSE)
  if (!acquired_lock && dir.exists(lock_dir)) {
    lock_age <- as.numeric(difftime(Sys.time(), file.info(lock_dir)$mtime, units = "secs"))
    if (!is.na(lock_age) && lock_age > 600) {
      unlink(lock_dir, recursive = TRUE)
      acquired_lock <- dir.create(lock_dir, showWarnings = FALSE)
    }
  }
  if (!acquired_lock) {
    deadline <- Sys.time() + 300
    while (Sys.time() < deadline && !file.exists(cache_file)) {
      Sys.sleep(1)
    }
    cached <- read_datasus_cache(cache_file)
    if (!is.null(cached)) {
      return(cached)
    }
    stop("Timed out waiting for concurrent DataSUS download: ", basename(cache_file))
  }
  on.exit(unlink(lock_dir, recursive = TRUE), add = TRUE)

  data <- fetch(
    year_start = year_start,
    year_end = year_end,
    uf = uf,
    information_system = information_system,
    stop_on_error = TRUE,
    timeout = 900
  )
  if (is.null(data)) {
    stop("DataSUS returned no data for the requested period and states")
  }

  temporary_file <- tempfile(pattern = "datasus-", tmpdir = cache_dir, fileext = ".rds")
  on.exit(unlink(temporary_file), add = TRUE)
  saveRDS(data, temporary_file, compress = "gzip")
  if (!file.rename(temporary_file, cache_file)) {
    stop("Unable to publish DataSUS cache file: ", cache_file)
  }
  prune_datasus_cache(cache_dir, cache_file)
  data
}
