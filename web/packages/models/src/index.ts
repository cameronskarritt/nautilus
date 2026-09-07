export interface HealthResponse {
  status: "ok" | "shutting_down"
}

export interface EnvResponse {
  version: number
  auth: {
    sso_providers: string[]
  }
}

export interface User {
  id: string
  email?: string
  username?: string
  auth_provider: "local" | "google" | "microsoft" | "github" | "apple"
  verified: boolean
  admin: boolean
  mfa_enabled: boolean
  created_at: string
}

export interface Organization {
  id: string
  slug: string
  name: string
  plan?: string
  personal: boolean
  created_at: string
}

export interface SessionResponse {
  user: User
  organization: Organization | null
  assumed: boolean
  flags: Record<string, boolean>
}

export interface Document {
  id: string
  filename: string
  content_type: string
  size: number
  created_at: string
  updated_at: string
}

export interface DocumentPage {
  data: Document[]
  has_more: boolean
  next_cursor?: string
}
