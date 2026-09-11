import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { isEditableTarget } from "@/lib/hotkeys/isEditableTarget";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";

function Harness({ onJ, enabled = true }: { onJ: () => void; enabled?: boolean }) {
  useHotkeys({ j: onJ }, { enabled });
  return (
    <div>
      <input aria-label="filter" />
      <textarea aria-label="notes" />
      <button type="button">plain</button>
    </div>
  );
}

describe("useHotkeys", () => {
  it("fires on a bare key press", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.keyboard("j");
    expect(onJ).toHaveBeenCalledTimes(1);
  });

  it("ignores unbound keys", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.keyboard("q");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("does not fire while an input has focus", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.click(screen.getByLabelText("filter"));
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("does not fire while a textarea has focus", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.click(screen.getByLabelText("notes"));
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("still fires when a plain button has focus", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.click(screen.getByRole("button", { name: "plain" }));
    await userEvent.keyboard("j");
    expect(onJ).toHaveBeenCalledTimes(1);
  });

  it.each(["{Control>}j{/Control}", "{Meta>}j{/Meta}", "{Alt>}j{/Alt}"])(
    "ignores %s so browser shortcuts keep working",
    async (sequence) => {
      const onJ = vi.fn();
      render(<Harness onJ={onJ} />);
      await userEvent.keyboard(sequence);
      expect(onJ).not.toHaveBeenCalled();
    },
  );

  it("does nothing when disabled", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} enabled={false} />);
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("uses the latest handler without resubscribing", async () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = render(<Harness onJ={first} />);
    rerender(<Harness onJ={second} />);
    await userEvent.keyboard("j");
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("detaches its listener on unmount", async () => {
    const onJ = vi.fn();
    const { unmount } = render(<Harness onJ={onJ} />);
    unmount();
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });
});

describe("isEditableTarget", () => {
  it("is false for null and for a plain div", () => {
    expect(isEditableTarget(null)).toBe(false);
    expect(isEditableTarget(document.createElement("div"))).toBe(false);
  });

  it.each(["input", "textarea", "select"])("is true for %s", (tag) => {
    expect(isEditableTarget(document.createElement(tag))).toBe(true);
  });

  it("is true for a contentEditable element", () => {
    const node = document.createElement("div");
    node.setAttribute("contenteditable", "true");
    document.body.append(node);
    expect(isEditableTarget(node)).toBe(true);
    node.remove();
  });
});
