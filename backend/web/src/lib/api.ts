// Hand-maintained TS shapes for the Go handler responses. Once the
// OpenAPI spec (optiqor-cli/docs/api/openapi.yaml) drives a generator
// this file goes away.

export type Severity = "HIGH" | "MED" | "LOW" | "INFO";
export type Confidence = "high" | "medium" | "low";
export type Category = "cost" | "security";

export type Finding = {
  DetectorID: string;
  Workload: string;
  Title: string;
  Detail: string;
  MonthlyUSDCents: number;
  Severity: Severity;
  Confidence: Confidence;
  Category: Category;
};

export type CostEstimate = {
  workload: string;
  region: string;
  replicas: number;
  cpu_millicores: number;
  memory_bytes: number;
  monthly_usd_cents: number;
  cpu_monthly_cents: number;
  mem_monthly_cents: number;
  note: string;
  accuracy_band_pct: number;
  unpriceable_field?: string;
};

export type AnalyzeResponse = {
  accuracy_disclosure: string;
  source: string;
  workloads_analyzed: number;
  findings: Finding[];
  cost_findings: Finding[];
  security_findings_bonus: Finding[];
  monthly_savings_usd: number;
  annual_savings_usd: number;
  cost_estimates?: CostEstimate[];
  share_hash: string;
  share_url: string;
};

// Empty default keeps calls same-origin so next.config.ts's rewrites
// (dev) or the reverse proxy (prod) handle them. Set the env var only
// for split-host setups (Next on Vercel, Go API on AWS).
export const apiBase =
  process.env.NEXT_PUBLIC_OPTIQOR_API ?? "";

export async function analyze(values: string): Promise<AnalyzeResponse> {
  const res = await fetch(`${apiBase}/v1/analyze`, {
    method: "POST",
    headers: { "Content-Type": "text/yaml" },
    body: values,
  });
  if (!res.ok) {
    throw await readApiError(res);
  }
  return (await res.json()) as AnalyzeResponse;
}

// Structured envelope rendered by internal/platform/httperr. The
// reader is tolerant: a server that hasn't been migrated yet still
// produces a usable ApiError via the plain-text fallback.
type ErrorEnvelope = {
  error?: {
    code?: string;
    message?: string;
    status_code?: number;
    request_id?: string;
    details?: Record<string, unknown>;
  };
};

async function readApiError(res: Response): Promise<ApiError> {
  const text = await res.text().catch(() => "");
  let code: string | undefined;
  let message = "";
  let requestId: string | undefined;
  let details: Record<string, unknown> | undefined;
  try {
    const env = JSON.parse(text) as ErrorEnvelope;
    if (env.error) {
      code = env.error.code;
      message = env.error.message ?? "";
      requestId = env.error.request_id;
      details = env.error.details;
    }
  } catch {
    message = text.trim();
  }
  if (!message) message = res.statusText || "request failed";
  return new ApiError(res.status, message, { code, requestId, details });
}

export class ApiError extends Error {
  status: number;
  code?: string;
  requestId?: string;
  details?: Record<string, unknown>;
  constructor(
    status: number,
    message: string,
    extras: { code?: string; requestId?: string; details?: Record<string, unknown> } = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = extras.code;
    this.requestId = extras.requestId;
    this.details = extras.details;
  }
}

// ---- Session / Whoami ------------------------------------------------

export type WhoamiResponse = {
  tenant_id: string;
  workspace_id?: string;
  subject?: string;
  name?: string;
  source: "jwt" | "header";
  expires_at?: string;
};

export async function fetchWhoami(headers?: HeadersInit): Promise<WhoamiResponse | null> {
  const res = await fetch(`${apiBase}/v1/session/whoami`, { headers, credentials: "include" });
  if (res.status === 401) return null;
  if (!res.ok) throw new ApiError(res.status, res.statusText);
  return (await res.json()) as WhoamiResponse;
}

// ---- Onboarding ------------------------------------------------------

export type OnboardingStage =
  | "signed_up"
  | "vcs_connected"
  | "repo_selected"
  | "first_pr_analyzed"
  | "agent_installed"
  | "first_apply_fix"
  | "first_receipt_issued";

export type OnboardingState = {
  current: OnboardingStage;
  reached_at: Record<OnboardingStage, string>;
  progress_percent: number;
  activated: boolean;
  activation_window: string;
  time_to_first_receipt?: { seconds: number; label: string };
  next_stage?: OnboardingStage;
  slos: {
    sandbox_latency: string;
    install_to_first_pr: string;
    install_to_first_reco: string;
    install_to_first_receipt: string;
  };
};

export const stageLabels: Record<OnboardingStage, string> = {
  signed_up: "Sign up",
  vcs_connected: "Connect VCS",
  repo_selected: "Select repo",
  first_pr_analyzed: "First PR analyzed",
  agent_installed: "Agent installed",
  first_apply_fix: "First Apply Fix merged",
  first_receipt_issued: "First Receipt issued",
};

export async function fetchOnboardingState(
  headers?: HeadersInit,
  signal?: AbortSignal,
): Promise<OnboardingState> {
  const res = await fetch(`${apiBase}/v1/onboarding/state`, {
    headers,
    credentials: "include",
    signal,
  });
  if (!res.ok) throw await readApiError(res);
  return (await res.json()) as OnboardingState;
}

export async function transitionOnboarding(
  to: OnboardingStage,
  headers?: HeadersInit,
): Promise<OnboardingState> {
  const res = await fetch(`${apiBase}/v1/onboarding/transition`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(headers ?? {}) },
    body: JSON.stringify({ to }),
  });
  if (!res.ok) throw await readApiError(res);
  return (await res.json()) as OnboardingState;
}

export function fmtUSD(cents: number): string {
  if (!cents) return "$0";
  const dollars = Math.floor(cents / 100);
  const rem = cents % 100;
  if (!rem) return `$${dollars.toLocaleString()}`;
  return `$${dollars.toLocaleString()}.${rem.toString().padStart(2, "0")}`;
}

// ---- Dashboard panels (Phase 5) -------------------------------------
// Backend handlers in internal/dashboard/handler.go. All three are
// tenant-scoped via the mux's tenant extractor; the dashboard's same-
// origin fetch picks up the session cookie automatically.

export type SavingsSummary = {
  lifetime_cents: number;
  month_to_date_cents: number;
  year_to_date_cents: number;
  merged_count: number;
  is_demo?: boolean;
  demo_disclaimer?: string;
};

export async function fetchSavingsSummary(
  headers?: HeadersInit,
  signal?: AbortSignal,
): Promise<SavingsSummary> {
  const res = await fetch(`${apiBase}/v1/savings/summary`, {
    headers,
    credentials: "include",
    signal,
  });
  if (!res.ok) throw await readApiError(res);
  return (await res.json()) as SavingsSummary;
}

export type ApplyFixItem = {
  id: string;
  repo: string;
  pr_url: string;
  state: "open" | "merged" | "closed" | "rolled-back";
  monthly_usd_cents: number;
  opened_at: string;
  merged_at?: string;
};

export type ApplyFixList = {
  items: ApplyFixItem[];
  next_cursor?: string;
};

export async function fetchApplyFixes(
  opts: { state?: ApplyFixItem["state"]; cursor?: string } = {},
  headers?: HeadersInit,
  signal?: AbortSignal,
): Promise<ApplyFixList> {
  const params = new URLSearchParams();
  if (opts.state) params.set("state", opts.state);
  if (opts.cursor) params.set("cursor", opts.cursor);
  const qs = params.toString();
  const res = await fetch(`${apiBase}/v1/apply-fixes${qs ? `?${qs}` : ""}`, {
    headers,
    credentials: "include",
    signal,
  });
  if (!res.ok) throw await readApiError(res);
  return (await res.json()) as ApplyFixList;
}

export type AgentHealth = {
  status: "healthy" | "degraded" | "offline" | "unknown";
  last_checkin: string;
  data_freshness_seconds: number;
  version?: string;
};

export async function fetchAgentHealth(
  headers?: HeadersInit,
  signal?: AbortSignal,
): Promise<AgentHealth> {
  const res = await fetch(`${apiBase}/v1/agent/health`, {
    headers,
    credentials: "include",
    signal,
  });
  if (!res.ok) throw await readApiError(res);
  return (await res.json()) as AgentHealth;
}

// Pretty-prints a relative duration ("3 min ago", "2 hr ago") that the
// agent health pill renders. Avoids importing a date library — three
// branches cover every Phase 5 case.
export function fmtRelative(iso: string, nowMs: number = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "—";
  const deltaMs = nowMs - t;
  if (deltaMs < 0) return "just now";
  const min = Math.floor(deltaMs / 60000);
  if (min < 1) return "just now";
  if (min < 60) return `${min} min ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr} hr ago`;
  const day = Math.floor(hr / 24);
  return `${day} day${day === 1 ? "" : "s"} ago`;
}
