// Typed client for the Optiqor backend API. Mirrors the Go handler
// shapes in backend/internal/sandbox/sandbox.go etc. The OpenAPI spec
// at optiqor-cli/docs/api/openapi.yaml will eventually generate this
// — until then it's hand-maintained so changes show up in TS review.

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

/**
 * apiBase — backend origin. Defaults to "" so the same Next.js
 * deployment can be reverse-proxied in front of the Go API; override
 * with NEXT_PUBLIC_OPTIQOR_API for split-host setups.
 */
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
    throw new ApiError(res.status, text || res.statusText);
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

export function fmtUSD(cents: number): string {
  if (!cents) return "$0";
  const dollars = Math.floor(cents / 100);
  const rem = cents % 100;
  if (!rem) return `$${dollars.toLocaleString()}`;
  return `$${dollars.toLocaleString()}.${rem.toString().padStart(2, "0")}`;
}
