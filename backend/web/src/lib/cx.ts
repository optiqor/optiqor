import clsx, { type ClassValue } from "clsx";

// Tiny wrapper so components don't pull clsx directly. Future-proofs a
// swap to tailwind-merge if we ever start needing class deduplication.
export function cx(...inputs: ClassValue[]): string {
  return clsx(inputs);
}
