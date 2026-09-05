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
