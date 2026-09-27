import { handlers } from "@/auth";

// Mounts every Auth.js OAuth/CSRF/session route under /api/auth/*.
export const { GET, POST } = handlers;
