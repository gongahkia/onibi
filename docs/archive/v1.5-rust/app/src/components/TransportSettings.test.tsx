import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { TransportSettings } from "./TransportSettings";

const statusPayload = [
  {
    name: "tailscale-funnel",
    label: "Tailscale Funnel",
    requiresExternalDep: "tailscale",
    enabled: false,
    status: { state: "stopped" },
    url: null,
    fingerprint: null,
  },
  {
    name: "cloudflared",
    label: "Cloudflare Tunnel",
    requiresExternalDep: "cloudflared",
    enabled: true,
    status: { state: "running", url: "https://abc.trycloudflare.com/" },
    url: "https://abc.trycloudflare.com/",
    fingerprint: null,
  },
  {
    name: "lan",
    label: "LAN",
    requiresExternalDep: null,
    enabled: false,
    status: { state: "stopped" },
    url: null,
    fingerprint: null,
  },
];

describe("TransportSettings", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn(async (url: string) => {
      if (url.includes("/enable")) {
        return { ok: true, status: 200, json: async () => statusPayload[0] };
      }
      return { ok: true, status: 200, json: async () => statusPayload };
    });
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  test("renders live status and enables a transport", async () => {
    render(<TransportSettings pollIntervalMs={60_000} />);

    expect(await screen.findByText("Tailscale Funnel")).toBeTruthy();
    expect(screen.getByText("Running at https://abc.trycloudflare.com/")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Enable Tailscale Funnel" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/transport/tailscale-funnel/enable",
        expect.objectContaining({ method: "POST" }),
      );
    });
  });
});
