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
        const capYear = loadedConfiguration.availableYears.at(-1);
        if (capYear) {
          const startYear = loadedConfiguration.availableYears.includes(capYear - 1)
            ? capYear - 1
            : loadedConfiguration.availableYears.at(0) ?? capYear;
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
            <p className="mt-2 text-latte-subtext0">Escolha o recorte dos dados e receba o PDF por e-mail.</p>
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
                    setSelectedReportId(event.target.value);
                    setSelectedStates([]);
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

              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Data inicial" id="start-date">
                  <Input id="start-date" className="h-11" max={maxDate} min={minDate} onChange={(event) => setStartDate(event.target.value)} required type="date" value={startDate} />
                </Field>
                <Field label="Data final" id="end-date">
                  <Input id="end-date" className="h-11" max={maxDate} min={startDate || minDate} onChange={(event) => setEndDate(event.target.value)} required type="date" value={endDate} />
                </Field>
              </div>

              <Field label="E-mail" id="email">
                <Input id="email" autoComplete="email" className="h-11" onChange={(event) => setEmail(event.target.value)} placeholder="pesquisador@exemplo.org" required type="email" value={email} />
              </Field>

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
              <h2 className="text-2xl font-extrabold" id="processed-reports-title">Relatórios processados</h2>
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
              Nenhum relatório foi processado ainda.
            </div>
          ) : (
            <div className="space-y-3">
              {reports.map((report) => (
                <Card className="rounded-2xl border-latte-surface1 bg-latte-base/70 p-5 shadow-sm" key={`${report.uri}-${report.data_processamento}`}>
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
                    <Button asChild variant="secondary" className="bg-latte-teal/15 text-latte-teal hover:bg-latte-teal/25">
                      <a href={apiUrl(report.uri)} rel="noreferrer" target="_blank">
                        Baixar PDF
                      </a>
                    </Button>
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
