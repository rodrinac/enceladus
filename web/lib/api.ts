export type StateOption = {
  nome: string;
  sigla: string;
};

export type ReportType = {
  campos_data: string[];
  colunas_mensais?: boolean;
  id: string;
  multiplos_estados: boolean;
  nome: string;
  parametros: string[];
  path: string;
};

export type ProcessedReport = {
  criado_em?: string | null;
  data_fim: string;
  data_inicio: string;
  data_processamento: string | null;
  estado: string;
  id_requisicao: string | null;
  mensagem: string | null;
  status: "failed" | "queued" | "running" | "succeeded";
  tipo: string;
  uri?: string;
};

export type ReportDateGranularity = "day" | "month" | "year";

export function reportDateGranularity(reportType: Pick<ReportType, "campos_data">): ReportDateGranularity {
  if (reportType.campos_data.includes("dd")) return "day";
  if (reportType.campos_data.includes("MM")) return "month";
  return "year";
}

type ReportRequest = {
  endDate: string;
  startDate: string;
  states: string[];
};

export type ReportSubmission =
  | { id_requisicao: string; status: "queued"; uri: null; reutilizado: false }
  | { id_requisicao: null; status: "succeeded"; uri: string; reutilizado: true };

const apiBaseUrl = process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, "") || "http://localhost:8000";

export function apiUrl(path: string): string {
  return `${apiBaseUrl}${path.startsWith("/") ? path : `/${path}`}`;
}

async function requestJson<T>(path: string, init?: RequestInit): Promise<T> {
  const requestUrl = /^https?:\/\//.test(path) ? path : apiUrl(path);
  const response = await fetch(requestUrl, init);

  if (!response.ok) {
    throw new Error(`A API respondeu com status ${response.status}.`);
  }

  return response.json() as Promise<T>;
}

export async function getConfiguration(): Promise<{
  availableYears: number[];
  reportTypes: ReportType[];
  states: StateOption[];
}> {
  const [availableYears, reportTypes, stateDtos] = await Promise.all([
    requestJson<number[]>("/config/anos"),
    requestJson<ReportType[]>("/config/relatorios"),
    requestJson<Record<string, string>[]>("/config/estados"),
  ]);

  const states = stateDtos.map((stateDto) => {
    const [sigla, nome] = Object.entries(stateDto)[0];
    return { nome, sigla };
  });

  return { availableYears, reportTypes, states };
}

export function getProcessedReports(signal?: AbortSignal): Promise<ProcessedReport[]> {
  return requestJson<ProcessedReport[]>("/relatorios/processados", { signal });
}

export async function requestReport(
  reportType: ReportType,
  reportRequest: ReportRequest,
): Promise<ReportSubmission> {
  const url = new URL(apiUrl(reportType.path));
  reportRequest.states.forEach((state) => url.searchParams.append("estado", state));

  if (reportDateGranularity(reportType) === "year") {
    url.searchParams.set("ano_inicio", reportRequest.startDate.slice(0, 4));
    url.searchParams.set("ano_fim", reportRequest.endDate.slice(0, 4));
  } else {
    url.searchParams.set("data_inicio", reportRequest.startDate);
    url.searchParams.set("data_fim", reportRequest.endDate);
  }

  return requestJson(url.toString(), { method: "POST" });
}

// subscribeToReports opens the SSE push stream (/relatorios/eventos) and
// forwards snapshots. Returns an unsubscribe function. Callers should keep
// polling getProcessedReports as a fallback when EventSource is unavailable.
export function subscribeToReports(onReports: (reports: ProcessedReport[]) => void): () => void {
  if (typeof EventSource === "undefined") return () => {};
  const source = new EventSource(apiUrl("/relatorios/eventos"));
  const handler = (event: MessageEvent) => {
    try {
      onReports(JSON.parse(event.data) as ProcessedReport[]);
    } catch {
      // Ignore malformed pushes; polling will repair the view.
    }
  };
  source.addEventListener("relatorios", handler as EventListener);
  return () => source.close();
}
