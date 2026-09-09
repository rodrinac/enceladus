export type StateOption = {
  nome: string;
  sigla: string;
};

export type ReportType = {
  campos_data: string[];
  id: string;
  multiplos_estados: boolean;
  nome: string;
  parametros: string[];
  path: string;
};

export type ProcessedReport = {
  data_fim: string;
  data_inicio: string;
  data_processamento: string | null;
  estado: string;
  tipo: string;
  uri: string;
};

type ReportRequest = {
  email: string;
  endDate: string;
  startDate: string;
  states: string[];
};

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
): Promise<{ destino: string; id_requisicao: string }> {
  const url = new URL(apiUrl(reportType.path));
  reportRequest.states.forEach((state) => url.searchParams.append("estado", state));

  if (reportType.id === "CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO") {
    url.searchParams.set("ano_inicio", reportRequest.startDate.slice(0, 4));
    url.searchParams.set("ano_fim", reportRequest.endDate.slice(0, 4));
  } else {
    url.searchParams.set("data_inicio", reportRequest.startDate);
    url.searchParams.set("data_fim", reportRequest.endDate);
  }

  url.searchParams.set("email", reportRequest.email);
  return requestJson(url.toString(), { method: "POST" });
}
