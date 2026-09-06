// バックエンド（internal/webapi）の DTO に対応する型。
// フィールド名・省略可否は Go 側の json タグに合わせる。

export type TurnStatus = 'asking' | 'proposed'

export interface Choice {
  question: string
  options: string[]
  multi: boolean
}

export interface Question {
  id: string
  choice: Choice
}

export interface Resource {
  id: string
  service: string
  label?: string
  parent?: string
  params?: Record<string, unknown>
}

export interface Edge {
  from: string
  to: string
  label?: string
}

export interface Assumptions {
  hoursPerDay: number
  daysPerMonth: number
  requestsPerMonth: number
  fxRate: number
  discountRate: number
  extra?: Record<string, number>
}

export interface Architecture {
  schemaVersion?: string
  provider: string
  region: string
  assumptions: Assumptions
  resources: Resource[]
  edges: Edge[]
}

export interface Turn {
  sessionId: string
  status: TurnStatus
  message?: string
  questions?: Question[]
  architecture?: Architecture
}

export interface Answer {
  selected: string[]
}

export interface Line {
  resourceId: string
  service: string
  driverId: string
  unit: string
  quantity: number
  unitPrice?: number
  currency?: string
  sku?: string
  fetchedAt?: string
  tiered?: boolean
  error?: string
}

export interface ArtifactInfo {
  format: string
  filename: string
  contentType: string
  downloadUrl: string
}

export interface EstimateResult {
  region: string
  lines: Line[]
  failedLines: number
  artifacts: ArtifactInfo[]
}
