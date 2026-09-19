import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiError } from "../api/client";
import { useConfirmAction } from "./useConfirmAction";

describe("useConfirmAction", () => {
  it("asking marks only that item as confirming, and cancel puts it back", () => {
    const { result } = renderHook(() => useConfirmAction());

    act(() => result.current.ask("row-1"));
    expect(result.current.isConfirming("row-1")).toBe(true);
    expect(result.current.isConfirming("row-2")).toBe(false);

    act(() => result.current.cancel());
    expect(result.current.isConfirming("row-1")).toBe(false);
  });

  it("a screen with only one thing to confirm can leave the key out", () => {
    const { result } = renderHook(() => useConfirmAction());

    act(() => result.current.ask());
    expect(result.current.isConfirming()).toBe(true);
  });

  it("is pending only while the action runs, and collapses the confirm pair once it succeeds", async () => {
    const { result } = renderHook(() => useConfirmAction());
    let finishAction: () => void = () => {};
    let confirming: Promise<void> = Promise.resolve();

    act(() => result.current.ask("row-1"));
    act(() => {
      confirming = result.current.confirm(
        () => new Promise<void>((resolve) => (finishAction = resolve)),
        "row-1",
      );
    });

    // Still asking while the request is out, so the disabled Confirm button
    // stays on screen instead of vanishing mid-request.
    expect(result.current.isPending("row-1")).toBe(true);
    expect(result.current.isConfirming("row-1")).toBe(true);

    await act(async () => {
      finishAction();
      await confirming;
    });

    expect(result.current.isPending("row-1")).toBe(false);
    expect(result.current.isConfirming("row-1")).toBe(false);
    expect(result.current.errorFor("row-1")).toBeNull();
  });

  it("a failure is recorded against that item alone, and the confirm pair still collapses", async () => {
    const { result } = renderHook(() => useConfirmAction("Fallback."));

    act(() => result.current.ask("row-1"));
    await act(() =>
      result.current.confirm(
        () => Promise.reject(new ApiError(404, "NOT_FOUND", "That row is already gone.")),
        "row-1",
      ),
    );

    expect(result.current.errorFor("row-1")).toBe("That row is already gone.");
    expect(result.current.errorFor("row-2")).toBeNull();
    expect(result.current.isConfirming("row-1")).toBe(false);
    expect(result.current.isPending("row-1")).toBe(false);
  });

  it("a failure that is not an ApiError shows the fallback message, never nothing", async () => {
    const { result } = renderHook(() => useConfirmAction("Could not remove that."));

    await act(() => result.current.confirm(() => Promise.reject(new TypeError("Failed to fetch"))));

    expect(result.current.errorFor()).toBe("Could not remove that.");
  });

  it("asking again keeps the earlier error; confirming again clears it before the new attempt", async () => {
    const { result } = renderHook(() => useConfirmAction());
    let finishRetry: () => void = () => {};
    let retrying: Promise<void> = Promise.resolve();

    await act(() => result.current.confirm(() => Promise.reject(new ApiError(409, "CONFLICT", "Refused."))));
    act(() => result.current.ask());
    expect(result.current.errorFor()).toBe("Refused.");

    act(() => {
      retrying = result.current.confirm(() => new Promise<void>((resolve) => (finishRetry = resolve)));
    });
    expect(result.current.errorFor()).toBeNull();

    await act(async () => {
      finishRetry();
      await retrying;
    });
  });

  it("a success on one row leaves another row's error where it is, and clearErrors removes every one", async () => {
    const { result } = renderHook(() => useConfirmAction());

    await act(() => result.current.confirm(() => Promise.reject(new ApiError(404, "NOT_FOUND", "Gone.")), "row-1"));
    await act(() => result.current.confirm(() => Promise.resolve(), "row-3"));
    expect(result.current.errorFor("row-1")).toBe("Gone.");

    act(() => result.current.clearErrors());
    expect(result.current.errorFor("row-1")).toBeNull();
  });
});
