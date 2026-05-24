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
    const text = await res.text().catch(() => "");
    // Surface the backend's error body verbatim when present — the
    // sandbox panel renders it inline.
    throw new ApiError(res.status, text.trim() || res.statusText);
  }
  return (await res.json()) as AnalyzeResponse;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
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

export async function fetchOnboardingState(headers?: HeadersInit): Promise<OnboardingState> {
  const res = await fetch(`${apiBase}/v1/onboarding/state`, { headers, credentials: "include" });
  if (!res.ok) throw new ApiError(res.status, res.statusText);
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
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new ApiError(res.status, text.trim() || res.statusText);
  }
  return (await res.json()) as OnboardingState;
}

export function fmtUSD(cents: number): string {
  if (!cents) return "$0";
  const dollars = Math.floor(cents / 100);
  const rem = cents % 100;
  if (!rem) return `$${dollars.toLocaleString()}`;
  return `$${dollars.toLocaleString()}.${rem.toString().padStart(2, "0")}`;
}
