import type { Answer, Architecture, EstimateResult, Turn } from './types'

const JSON_HEADERS = { 'Content-Type': 'application/json' }

class ApiError extends Error {}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body?.error) message = body.error
    } catch {
      // レスポンスが JSON でなければステータス行だけ使う。
    }
    throw new ApiError(message)
  }
  return (await res.json()) as T
}

export function startSession(message: string): Promise<Turn> {
  return call<Turn>('/api/sessions', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ message }),
  })
}

export function sendMessage(sessionId: string, message: string): Promise<Turn> {
  return call<Turn>(`/api/sessions/${encodeURIComponent(sessionId)}/messages`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ message }),
  })
}

export function sendAnswers(sessionId: string, answers: Record<string, Answer>): Promise<Turn> {
  return call<Turn>(`/api/sessions/${encodeURIComponent(sessionId)}/answers`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ answers }),
  })
}

export function saveArchitecture(
  architecture: Architecture,
): Promise<{ id: string; architecture: Architecture }> {
  return call('/api/architectures', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ architecture }),
  })
}

export function loadArchitecture(
  id: string,
): Promise<{ id: string; architecture: Architecture; createdAt: string }> {
  return call(`/api/architectures/${encodeURIComponent(id)}`)
}

export function runEstimate(id: string): Promise<EstimateResult> {
  return call<EstimateResult>(`/api/architectures/${encodeURIComponent(id)}/estimate`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({}),
  })
}

export { ApiError }
