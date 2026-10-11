import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import App from "./App";

describe("App shell", () => {
  it("shows a not-yet-implemented status banner", () => {
    render(<App />);
    const banner = screen.getByRole("status");
    expect(banner.textContent).toMatch(/not yet implemented/i);
  });

  it("exposes a main landmark and a Sentinel heading", () => {
    render(<App />);
    expect(screen.getByRole("main")).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Sentinel", level: 1 }),
    ).toBeTruthy();
  });

  it("makes no network calls and renders no alert data", () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    render(<App />);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(screen.queryAllByRole("row")).toHaveLength(0);
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    vi.unstubAllGlobals();
  });
});
