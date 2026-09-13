"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";

import {
  apiUrl,
  getConfiguration,
  getProcessedReports,
  ProcessedReport,
  reportDateGranularity,
  ReportType,
  requestReport,
  StateOption,
  subscribeToReports,
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

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat("pt-BR", { dateStyle: "short", timeStyle: "medium" }).format(new Date(value));
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
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);

  const selectedReport = useMemo(
    () => configuration?.reportTypes.find(({ id }) => id === selectedReportId) ?? null,
    [configuration, selectedReportId],
  );

  const monthlyColumns = selectedReport?.colunas_mensais ?? false;
  const granularity = selectedReport ? reportDateGranularity(selectedReport) : "day";
  const inputLength = granularity === "month" ? 7 : 10;

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
        const initialReport = loadedConfiguration.reportTypes[0] ?? null;
        setSelectedReportId(initialReport?.id ?? "");
        const capYear = loadedConfiguration.availableYears.at(-1);
        if (capYear) {
          const twoYearStart = loadedConfiguration.availableYears.includes(capYear - 1)
            ? capYear - 1
            : loadedConfiguration.availableYears.at(0) ?? capYear;
          const startYear = initialReport?.colunas_mensais ? capYear : twoYearStart;
          setStartDate(`${startYear}-01-01`);
          setEndDate(`${capYear}-12-31`);
        }
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
    // Push updates via SSE; the interval below stays as a fallback for
    // browsers/proxies without EventSource support.
    const unsubscribe = subscribeToReports((pushedReports) => {
      setReports(pushedReports);
      setReportsError(null);
      setReportsLoading(false);
    });
    const interval = window.setInterval(() => void refreshReports(), 10_000);

    return () => {
      controller.abort();
      unsubscribe();
      window.clearInterval(interval);
    };
  }, [refreshReports]);

  const firstYear = configuration?.availableYears.at(0);
  const lastYear = configuration?.availableYears.at(-1);
  const minDate = firstYear ? `${firstYear}-01-01` : undefined;
  const maxDate = lastYear ? `${lastYear}-12-31` : undefined;

  const startYear = startDate.slice(0, 4);
  const endYear = endDate.slice(0, 4);
  const startMinDate = monthlyColumns && endYear ? `${endYear}-01-01` : minDate;
  const endMaxDate = monthlyColumns && startYear ? `${startYear}-12-31` : maxDate;

  function handleStartDateChange(value: string) {
    setStartDate(value);
    if (monthlyColumns && value) {
      const year = value.slice(0, 4);
      setEndDate((current) => (current && current.slice(0, 4) !== year ? `${year}-12-31` : current));
    }
  }

  function handleEndDateChange(value: string) {
    setEndDate(value);
    if (monthlyColumns && value) {
      const year = value.slice(0, 4);
      setStartDate((current) => (current && current.slice(0, 4) !== year ? `${year}-01-01` : current));
    }
  }

  function handleYearChange(year: string) {
    setStartDate(`${year}-01-01`);
    setEndDate(`${year}-12-31`);
  }

  function onStartDateInput(value: string) {
    handleStartDateChange(granularity === "month" && value ? `${value}-01` : value);
  }

  function onEndDateInput(value: string) {
    handleEndDateChange(granularity === "month" && value ? `${value}-01` : value);
  }

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
        endDate,
        startDate,
        states: selectedStates,
      });
      if (response.reutilizado) {
        setNotice({
          kind: "success",
          message: "Relatório já processado — download imediato disponível abaixo.",
        });
      } else {
        setNotice({
          kind: "success",
          message: `Relatório enviado para processamento. Código: ${response.id_requisicao}`,
        });
      }
      void refreshReports();
    } catch {
      setNotice({ kind: "error", message: "Não foi possível solicitar o relatório." });
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <main className="min-h-screen text-latte-text">
      <header className="glass-panel border-b border-latte-surface0 bg-latte-mantle">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-5 py-5 sm:px-8">
          <div>
            <p className="text-sm font-bold uppercase tracking-[0.2em] text-latte-blue">Enceladus</p>
            <h1 className="text-xl font-extrabold sm:text-2xl">Inteligência sobre queimaduras</h1>
          </div>
          <Badge className="hidden rounded-full bg-latte-green/15 px-3 py-1 text-sm font-bold text-latte-green sm:inline">
            Dados para pesquisa
          </Badge>
        </div>
      </header>

      <div className="mx-auto grid max-w-7xl gap-8 px-5 py-8 sm:px-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)] lg:py-12">
        <Card className="glass-panel gap-0 self-start rounded-3xl p-6 sm:p-8">
          <div className="mb-7">
            <p className="mb-2 text-sm font-bold text-latte-mauve">NOVO PEDIDO</p>
            <h2 className="text-2xl font-extrabold">Solicitar relatório</h2>
            <p className="mt-2 text-latte-subtext0">Escolha o recorte dos dados e acompanhe o progresso aqui — avisamos quando o PDF estiver pronto.</p>
          </div>

          {configurationError ? (
            <StatusMessage kind="error">{configurationError}</StatusMessage>
          ) : !configuration ? (
            <StatusMessage>Carregando opções…</StatusMessage>
          ) : (
            <form className="space-y-6" onSubmit={submit}>
              <Field label="Tipo de relatório" id="report-type">
                <NativeSelect
                  id="report-type"
                  className="h-11"
                  onChange={(event) => {
                    const nextReportType = configuration.reportTypes.find(({ id }) => id === event.target.value);
                    setSelectedReportId(event.target.value);
                    setSelectedStates([]);
                    if (nextReportType?.colunas_mensais && startYear !== endYear) {
                      setStartDate(`${endYear}-01-01`);
                      setEndDate(`${endYear}-12-31`);
                    }
                  }}
                  value={selectedReportId}
                >
                  {configuration.reportTypes.map((reportType) => (
                    <NativeSelectOption key={reportType.id} value={reportType.id}>{reportType.nome}</NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>

              <fieldset>
                <legend className="mb-3 font-bold">Estados</legend>
                <div className="mb-3 flex items-center justify-between gap-3">
                  {selectedReport?.multiplos_estados && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="ml-auto text-primary"
                      onClick={() => setSelectedStates(
                        selectedStates.length === configuration.states.length
                          ? []
                          : configuration.states.map(({ sigla }) => sigla),
                      )}
                      type="button"
                    >
                      {selectedStates.length === configuration.states.length ? "Limpar" : "Selecionar todos"}
                    </Button>
                  )}
                </div>
                <StateSelection
                  multiple={selectedReport?.multiplos_estados ?? false}
                  value={selectedStates[0] ?? ""}
                  onValueChange={(state) => setSelectedStates([state])}
                >
                  {configuration.states.map((state) => {
                    const checked = selectedStates.includes(state.sigla);
                    return (
                      <Label
                        htmlFor={`state-${state.sigla}`}
                        className={`flex cursor-pointer items-center gap-2 rounded-xl px-3 py-2 text-sm transition ${
                          checked ? "bg-latte-blue/10 text-primary" : "hover:bg-latte-surface0"
                        }`}
                        key={state.sigla}
                      >
                        {selectedReport?.multiplos_estados ? (
                          <Checkbox id={`state-${state.sigla}`} checked={checked} name="estado" onCheckedChange={() => toggleState(state.sigla)} value={state.sigla} />
                        ) : (
                          <RadioGroupItem id={`state-${state.sigla}`} value={state.sigla} />
                        )}
                        <span>{state.nome}</span>
                      </Label>
                    );
                  })}
                </StateSelection>
              </fieldset>

              {granularity === "year" ? (
                <Field label="Ano" id="report-year">
                  <NativeSelect
                    id="report-year"
                    className="h-11"
                    onChange={(event) => handleYearChange(event.target.value)}
                    value={startYear}
                  >
                    {configuration.availableYears.map((year) => (
                      <NativeSelectOption key={year} value={String(year)}>{year}</NativeSelectOption>
                    ))}
                  </NativeSelect>
                </Field>
              ) : (
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Data inicial" id="start-date">
                    <Input
                      id="start-date"
                      className="h-11"
                      max={maxDate?.slice(0, inputLength)}
                      min={startMinDate?.slice(0, inputLength)}
                      onChange={(event) => onStartDateInput(event.target.value)}
                      required
                      type={granularity === "month" ? "month" : "date"}
                      value={startDate.slice(0, inputLength)}
                    />
                  </Field>
                  <Field label="Data final" id="end-date">
                    <Input
                      id="end-date"
                      className="h-11"
                      max={endMaxDate?.slice(0, inputLength)}
                      min={(startDate || minDate)?.slice(0, inputLength)}
                      onChange={(event) => onEndDateInput(event.target.value)}
                      required
                      type={granularity === "month" ? "month" : "date"}
                      value={endDate.slice(0, inputLength)}
                    />
                  </Field>
                </div>
              )}

              {notice && <StatusMessage kind={notice.kind}>{notice.message}</StatusMessage>}

              <Button className="h-12 w-full rounded-xl font-extrabold" disabled={isSubmitting} type="submit">
                {isSubmitting ? "Enviando…" : "Processar relatório"}
              </Button>
            </form>
          )}
        </Card>

        <section
          aria-labelledby="processed-reports-title"
          className="glass-panel self-start rounded-3xl border border-latte-surface0 bg-card p-6 sm:p-8"
        >
          <div className="mb-5 flex items-end justify-between gap-4">
            <div>
              <p className="mb-2 text-sm font-bold text-latte-teal">ARQUIVO</p>
              <h2 className="text-2xl font-extrabold" id="processed-reports-title">Relatórios</h2>
            </div>
            <Button variant="outline" className="bg-card" onClick={() => void refreshReports()} type="button">
              Atualizar
            </Button>
          </div>

          {reportsError && <StatusMessage kind="error">{reportsError}</StatusMessage>}
          {reportsLoading ? (
            <StatusMessage>Carregando relatórios…</StatusMessage>
          ) : reports.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-latte-overlay0 bg-latte-mantle p-10 text-center text-latte-subtext0">
              Nenhum relatório foi solicitado ainda.
            </div>
          ) : (
            <div className="space-y-3">
              {reports.map((report) => (
                <Card
                  className="rounded-2xl border-latte-surface1 bg-latte-base/70 p-5 shadow-sm"
                  key={report.id_requisicao ?? report.uri ?? `${report.tipo}-${report.data_processamento}`}
                >
                  <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
                    <div>
                      <h3 className="font-extrabold">{report.tipo}</h3>
                      <p className="mt-1 text-sm text-latte-subtext0">
                        {report.estado} · {formatDate(report.data_inicio)} — {formatDate(report.data_fim)}
                      </p>
                      <p className="mt-2 text-xs font-bold uppercase tracking-wide text-latte-overlay1">
                        {report.status === "succeeded"
                          ? `Processado em ${report.data_processamento ? formatDateTime(report.data_processamento) : "data indisponível"}`
                          : `Solicitado em ${report.criado_em ? formatDateTime(report.criado_em) : ""}`}
                      </p>
                      {report.mensagem && <p className="mt-2 text-sm text-latte-red">{report.mensagem}</p>}
                    </div>
                    <div className="flex items-center gap-3">
                      <ReportStatusBadge status={report.status} />
                      {report.status === "succeeded" && report.uri && (
                        <Button asChild variant="secondary" className="bg-latte-teal/15 text-latte-teal hover:bg-latte-teal/25">
                          <a href={apiUrl(report.uri)} rel="noreferrer" target="_blank">
                            Baixar PDF
                          </a>
                        </Button>
                      )}
                    </div>
                  </div>
                </Card>
              ))}
            </div>
          )}
        </section>
      </div>

      <footer className="glass-panel border-t border-latte-surface0 bg-latte-mantle px-5 py-6 text-center text-sm text-latte-subtext0">
        Enceladus Big Data · Sociedade Brasileira de Queimaduras
      </footer>
    </main>
  );
}

function ReportStatusBadge({ status }: Readonly<{ status: ProcessedReport["status"] }>) {
  const labels = {
    failed: "Falhou",
    queued: "Na fila",
    running: "Processando",
    succeeded: "Processado",
  };
  const styles = {
    failed: "bg-latte-red/15 text-latte-red",
    queued: "bg-latte-peach/15 text-latte-peach",
    running: "bg-latte-blue/15 text-latte-blue",
    succeeded: "bg-latte-green/15 text-latte-green",
  };

  return <Badge className={`rounded-full ${styles[status]}`}>{labels[status]}</Badge>;
}

function Field({ children, label, id }: Readonly<{ children: React.ReactNode; label: string; id: string }>) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id} className="font-bold">{label}</Label>
      {children}
    </div>
  );
}

function StateSelection({ children, multiple, value, onValueChange }: Readonly<{
  children: React.ReactNode;
  multiple: boolean;
  value: string;
  onValueChange: (state: string) => void;
}>) {
  const className = "grid max-h-56 grid-cols-2 gap-2 overflow-y-auto rounded-2xl border border-input bg-background p-3 sm:grid-cols-3";

  return multiple ? (
    <div className={className}>{children}</div>
  ) : (
    <RadioGroup aria-label="Estados" className={className} value={value} onValueChange={onValueChange}>
      {children}
    </RadioGroup>
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

  return (
    <Alert role={kind === "error" ? "alert" : "status"} className={`rounded-xl ${styles[kind]}`}>
      <AlertDescription className="font-bold text-inherit">{children}</AlertDescription>
    </Alert>
  );
}
