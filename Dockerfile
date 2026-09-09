# syntax=docker/dockerfile:1

FROM rocker/r-ver:4.5.3

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=pt_BR.UTF-8 \
    LC_ALL=pt_BR.UTF-8 \
    PATH=/opt/venv/bin:$PATH \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        curl \
        libcurl4-openssl-dev \
        libfontconfig1-dev \
        libfribidi-dev \
        libharfbuzz-dev \
        libssl-dev \
        libxml2-dev \
        lmodern \
        locales \
        pandoc \
        python3 \
        python3-pip \
        python3-venv \
        texlive-fonts-recommended \
        texlive-latex-base \
        texlive-latex-extra \
        texlive-latex-recommended \
    && locale-gen pt_BR.UTF-8 \
    && rm -rf /var/lib/apt/lists/*

RUN install2.r --error --skipinstalled \
      gridExtra \
      janitor \
      kableExtra \
      matrixStats \
      remotes \
    && Rscript -e 'remotes::install_github("rfsaldanha/microdatasus", ref = "7109ec2c42cf674ba453e0d7a20d2f464890b543", upgrade = "never")' \
    && Rscript -e 'stopifnot(requireNamespace("microdatasus", quietly = TRUE))' \
    && rm -rf /tmp/downloaded_packages

WORKDIR /app

COPY pyproject.toml README.md ./
COPY src ./src
COPY docker/ibge ./docker/ibge

RUN python3 -m venv /opt/venv \
    && python -m pip install . \
    && useradd --create-home --uid 10001 enceladus \
    && mkdir --parents /var/lib/enceladus \
    && chown --recursive enceladus:enceladus /var/lib/enceladus

ENV ENCELADUS_POPULATION_DATA_PATH=/var/lib/enceladus/data/population.csv \
    REDIS_HOST=redis

USER enceladus
WORKDIR /app/src

EXPOSE 8000

HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=5 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8000/health', timeout=3)"

CMD ["hypercorn", "--bind", "0.0.0.0:8000", "main:app"]
