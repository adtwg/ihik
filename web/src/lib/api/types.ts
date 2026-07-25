export type Role = "super_admin" | "mitra";

export type Principal = {
  id: string;
  tenant_id: string | null;
  username: string;
  role: Role;
};

export type APIErrorBody = {
  error?: { code?: string; message?: string };
};

export class APIError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
  }
}