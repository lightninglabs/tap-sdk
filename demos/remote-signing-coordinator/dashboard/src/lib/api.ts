import type {
  CoordinatorConfig,
  Session,
  StartSessionRequest,
} from "@/lib/types";

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export async function getConfig(): Promise<CoordinatorConfig> {
  return request<CoordinatorConfig>("/api/config");
}

export async function getSessions(): Promise<Session[]> {
  return request<Session[]>("/api/sessions");
}

export async function getSession(id: string): Promise<Session> {
  return request<Session>(`/api/sessions/${id}`);
}

export async function createSession(
  payload: StartSessionRequest,
): Promise<Session> {
  return request<Session>("/api/sessions", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export async function submitSignature(
  id: string,
  signedVirtualPSBT: string,
): Promise<Session> {
  return request<Session>(`/api/sessions/${id}/signature`, {
    method: "POST",
    body: JSON.stringify({
      signed_virtual_psbt: signedVirtualPSBT,
    }),
  });
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });

  const body = await response.json().catch(() => null);
  if (!response.ok) {
    const message = body?.error ?? `${response.status} ${response.statusText}`;
    throw new ApiError(response.status, message);
  }

  return body as T;
}
