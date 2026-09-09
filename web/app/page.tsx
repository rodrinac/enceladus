"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";

import {
  apiUrl,
  getConfiguration,
  getProcessedReports,
  ProcessedReport,
  ReportType,
  requestReport,
  StateOption,
} from "@/lib/api";

type Configuration = {
  availableYears: number[];
  reportTypes: ReportType[];
  states: StateOption[];
};

type Notice = { kind: "error" | "success"; message: string } | null;

function formatDate(value: string): string {
  if (/^\d{4}$/.test(value)) return value;

  return new Intl.DateTimeFormat("pt-BR", { timeZone: "UTC" }).format(new Date(value));
}

export default function Home() {
  const [configuration, setConfiguration] = useState<Configuration | null>(null);
  const [configurationError, setConfigurationError] = useState<string | null>(null);
  const [reports, setReports] = useState<ProcessedReport[]>([]);
  const [reportsError, setReportsError] = useState<string | null>(null);
  const [reportsLoading, setReportsLoading] = useState(true);
  const [selectedReportId, setSelectedReportId] = useState("");
  const [selectedStates, setSelectedStates] = useState<string[]>([]);
  const [startDate, setStartDate] = useState("");
  const [endDate, setEndDate] = useState("");
  const [email, setEmail] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);

  const selectedReport = useMemo(
    () => configuration?.reportTypes.find(({ id }) => id === selectedReportId) ?? null,
    [configuration, selectedReportId],
  );

  const refreshReports = useCallback(async (signal?: AbortSignal) => {
    try {
      setReports(await getProcessedReports(signal));
      setReportsError(null);
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      setReportsError("Não foi possível atualizar os relatórios processados.");
    } finally {
      setReportsLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();

    getConfiguration()
      .then((loadedConfiguration) => {
        setConfiguration(loadedConfiguration);
        setSelectedReportId(loadedConfiguration.reportTypes[0]?.id ?? "");
      })
      .catch(() => setConfigurationError("Não foi possível carregar a configuração da API."));

    getProcessedReports(controller.signal)
      .then((processedReports) => {
        setReports(processedReports);
        setReportsError(null);
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return;
        setReportsError("Não foi possível atualizar os relatórios processados.");
      })
      .finally(() => setReportsLoading(false));
    const interval = window.setInterval(() => void refreshReports(), 10_000);

    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, [refreshReports]);

  const firstYear = configuration?.availableYears.at(0);
  const lastYear = configuration?.availableYears.at(-1);
  const minDate = firstYear ? `${firstYear}-01-01` : undefined;
  const maxDate = lastYear ? `${lastYear}-12-31` : undefined;

  function toggleState(state: string) {
    if (!selectedReport?.multiplos_estados) {
      setSelectedStates([state]);
      return;
    }

    setSelectedStates((current) =>
      current.includes(state) ? current.filter((item) => item !== state) : [...current, state],
    );
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedReport || selectedStates.length === 0) {
      setNotice({ kind: "error", message: "Selecione pelo menos um estado." });
      return;
    }

    setIsSubmitting(true);
    setNotice(null);

    try {
      const response = await requestReport(selectedReport, {
        email,
        endDate,
        startDate,
        states: selectedStates,
      });
      setNotice({
        kind: "success",
        message: `Relatório enviado para processamento. Código: ${response.id_requisicao}`,
      });
    } catch {
      setNotice({ kind: "error", message: "Não foi possível solicitar o relatório." });
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <main className="min-h-screen bg-latte-base text-latte-text">
      <header className="border-b border-latte-surface0 bg-latte-mantle">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-5 py-5 sm:px-8">
          <div>
            <p className="text-sm font-bold uppercase tracking-[0.2em] text-latte-blue">Enceladus</p>
            <h1 className="text-xl font-extrabold sm:text-2xl">Inteligência sobre queimaduras</h1>
          </div>
          <span className="hidden rounded-full bg-latte-green/15 px-3 py-1 text-sm font-bold text-latte-green sm:inline">
            Dados para pesquisa
          </span>
        </div>
      </header>

      <div className="mx-auto grid max-w-7xl gap-8 px-5 py-8 sm:px-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)] lg:py-12">
        <section className="rounded-3xl border border-latte-surface0 bg-white p-6 shadow-sm sm:p-8">
          <div className="mb-7">
            <p className="mb-2 text-sm font-bold text-latte-mauve">NOVO PEDIDO</p>
            <h2 className="text-2xl font-extrabold">Solicitar relatório</h2>
            <p className="mt-2 text-latte-subtext0">Escolha o recorte dos dados e receba o PDF por e-mail.</p>
          </div>

          {configurationError ? (
            <StatusMessage kind="error">{configurationError}</StatusMessage>
          ) : !configuration ? (
            <StatusMessage>Carregando opções…</StatusMessage>
          ) : (
            <form className="space-y-6" onSubmit={submit}>
              <Field label="Tipo de relatório">
                <select
                  className="control"
                  onChange={(event) => {
                    setSelectedReportId(event.target.value);
                    setSelectedStates([]);
                  }}
                  value={selectedReportId}
                >
                  {configuration.reportTypes.map((reportType) => (
                    <option key={reportType.id} value={reportType.id}>{reportType.nome}</option>
                  ))}
                </select>
              </Field>

              <fieldset>
                <div className="mb-3 flex items-center justify-between gap-3">
                  <legend className="font-bold">Estados</legend>
                  {selectedReport?.multiplos_estados && (
                    <button
                      className="text-sm font-bold text-latte-blue hover:text-latte-sapphire"
                      onClick={() => setSelectedStates(
                        selectedStates.length === configuration.states.length
                          ? []
                          : configuration.states.map(({ sigla }) => sigla),
                      )}
                      type="button"
                    >
                      {selectedStates.length === configuration.states.length ? "Limpar" : "Selecionar todos"}
                    </button>
                  )}
                </div>
                <div className="grid max-h-56 grid-cols-2 gap-2 overflow-y-auto rounded-2xl border border-latte-surface1 bg-latte-base p-3 sm:grid-cols-3">
                  {configuration.states.map((state) => {
                    const checked = selectedStates.includes(state.sigla);
                    return (
                      <label
                        className={`flex cursor-pointer items-center gap-2 rounded-xl px-3 py-2 text-sm transition ${
                          checked ? "bg-latte-blue text-white" : "hover:bg-latte-surface0"
                        }`}
                        key={state.sigla}
                      >
                        <input
                          checked={checked}
                          className="accent-latte-blue"
                          name="estado"
                          onChange={() => toggleState(state.sigla)}
                          type={selectedReport?.multiplos_estados ? "checkbox" : "radio"}
                          value={state.sigla}
                        />
                        <span>{state.nome}</span>
                      </label>
                    );
                  })}
                </div>
              </fieldset>

              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Data inicial">
                  <input className="control" max={maxDate} min={minDate} onChange={(event) => setStartDate(event.target.value)} required type="date" value={startDate} />
                </Field>
                <Field label="Data final">
                  <input className="control" max={maxDate} min={startDate || minDate} onChange={(event) => setEndDate(event.target.value)} required type="date" value={endDate} />
                </Field>
              </div>

              <Field label="E-mail">
                <input autoComplete="email" className="control" onChange={(event) => setEmail(event.target.value)} placeholder="pesquisador@exemplo.org" required type="email" value={email} />
              </Field>

              {notice && <StatusMessage kind={notice.kind}>{notice.message}</StatusMessage>}

              <button className="w-full rounded-xl bg-latte-blue px-5 py-3 font-extrabold text-white transition hover:bg-latte-sapphire disabled:cursor-wait disabled:opacity-60" disabled={isSubmitting} type="submit">
                {isSubmitting ? "Enviando…" : "Processar relatório"}
              </button>
            </form>
          )}
        </section>

        <section aria-labelledby="processed-reports-title">
          <div className="mb-5 flex items-end justify-between gap-4">
            <div>
              <p className="mb-2 text-sm font-bold text-latte-teal">ARQUIVO</p>
              <h2 className="text-2xl font-extrabold" id="processed-reports-title">Relatórios processados</h2>
            </div>
            <button className="rounded-xl border border-latte-surface1 bg-white px-4 py-2 text-sm font-bold hover:bg-latte-mantle" onClick={() => void refreshReports()} type="button">
              Atualizar
            </button>
          </div>

          {reportsError && <StatusMessage kind="error">{reportsError}</StatusMessage>}
          {reportsLoading ? (
            <StatusMessage>Carregando relatórios…</StatusMessage>
          ) : reports.length === 0 ? (
            <div className="rounded-3xl border border-dashed border-latte-overlay0 bg-latte-mantle p-10 text-center text-latte-subtext0">
              Nenhum relatório foi processado ainda.
            </div>
          ) : (
            <div className="space-y-3">
              {reports.map((report) => (
                <article className="rounded-2xl border border-latte-surface0 bg-white p-5 shadow-sm" key={`${report.uri}-${report.data_processamento}`}>
                  <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
                    <div>
                      <h3 className="font-extrabold">{report.tipo}</h3>
                      <p className="mt-1 text-sm text-latte-subtext0">
                        {report.estado} · {formatDate(report.data_inicio)} — {formatDate(report.data_fim)}
                      </p>
                      <p className="mt-2 text-xs font-bold uppercase tracking-wide text-latte-overlay1">
                        Processado em {report.data_processamento ?? "data indisponível"}
                      </p>
                    </div>
                    <a className="shrink-0 rounded-xl bg-latte-teal/15 px-4 py-2 text-center text-sm font-extrabold text-latte-teal hover:bg-latte-teal/25" href={apiUrl(report.uri)} rel="noreferrer" target="_blank">
                      Baixar PDF
                    </a>
                  </div>
                </article>
              ))}
            </div>
          )}
        </section>
      </div>

      <footer className="border-t border-latte-surface0 bg-latte-mantle px-5 py-6 text-center text-sm text-latte-subtext0">
        Enceladus Big Data · Sociedade Brasileira de Queimaduras
      </footer>
    </main>
  );
}

function Field({ children, label }: Readonly<{ children: React.ReactNode; label: string }>) {
  return (
    <label className="block">
      <span className="mb-2 block font-bold">{label}</span>
      {children}
    </label>
  );
}

function StatusMessage({
  children,
  kind = "neutral",
}: Readonly<{
  children: React.ReactNode;
  kind?: "error" | "neutral" | "success";
}>) {
  const styles = {
    error: "border-latte-red/30 bg-latte-red/10 text-latte-red",
    neutral: "border-latte-surface1 bg-latte-mantle text-latte-subtext0",
    success: "border-latte-green/30 bg-latte-green/10 text-latte-green",
  };

  return <p className={`rounded-xl border p-4 text-sm font-bold ${styles[kind]}`}>{children}</p>;
}
