import type { SessionStatus } from "@/lib/types";

export function shortValue(value?: string, head = 10, tail = 8): string {
  if (!value) {
    return "pending";
  }
  if (value.length <= head + tail + 3) {
    return value;
  }

  return `${value.slice(0, head)}...${value.slice(-tail)}`;
}

export function formatAmount(value?: number): string {
  if (value === undefined) {
    return "0";
  }

  return new Intl.NumberFormat("en-US").format(value);
}

export function formatTime(value: string): string {
  return new Intl.DateTimeFormat("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

export function statusLabel(status: SessionStatus): string {
  switch (status) {
    case "staging":
      return "Staging";
    case "waiting_signature":
      return "Waiting for signature";
    case "signature_submitted":
      return "Signature submitted";
    case "finalized":
      return "Finalized";
    case "failed":
      return "Failed";
  }
}
