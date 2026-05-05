/**
 * Tests for `components/dashboard/alert-action-menu.tsx`.
 *
 * Asserts that all four actions render and that the click handlers
 * wire up correctly — particularly the side-effects we own:
 *   - Mute optimistically calls `onMute(alertId, ms)`
 *   - Copy invokes `navigator.clipboard.writeText`
 *
 * The Radix dropdown opens content into a portal, so we use
 * `userEvent.click` on the trigger rather than asserting layout.
 */

import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";

// User-event is not available in this workspace; fireEvent is enough
// for click + keyboard cases here.

// The shared ui-components package uses Node "exports" subpaths that
// Jest doesn't resolve out of the box. Mock the dropdown-menu primitive
// with a flat-renderer stand-in: the trigger is a button and content is
// rendered inline (not in a portal) so findByText works without
// Radix's portal lifecycle. The contract under test is:
//   - the trigger fires onClick (we just need it to be a button)
//   - menu items run their onClick handlers
//   - asChild items inline their child element (e.g. an <a>)
// which this stub honours.
jest.mock("@enclii/ui-components/dropdown-menu", () => {
  const Passthrough = ({ children }: { children?: React.ReactNode }) => (
    <>{children}</>
  );
  const Item = ({
    children,
    onClick,
    asChild,
    className,
  }: {
    children?: React.ReactNode;
    onClick?: () => void;
    asChild?: boolean;
    className?: string;
  }) => {
    if (asChild && React.isValidElement(children)) {
      return children;
    }
    return (
      <button type="button" onClick={onClick} className={className} role="menuitem">
        {children}
      </button>
    );
  };
  const Trigger = ({
    children,
    asChild,
    onClick,
    className,
    ...rest
  }: React.ButtonHTMLAttributes<HTMLButtonElement> & {
    asChild?: boolean;
  }) => {
    if (asChild && React.isValidElement(children)) {
      return children;
    }
    return (
      <button type="button" onClick={onClick} className={className} {...rest}>
        {children}
      </button>
    );
  };
  return {
    DropdownMenu: Passthrough,
    DropdownMenuTrigger: Trigger,
    DropdownMenuContent: Passthrough,
    DropdownMenuItem: Item,
    DropdownMenuSeparator: () => <hr />,
    DropdownMenuLabel: Passthrough,
    DropdownMenuGroup: Passthrough,
    DropdownMenuPortal: Passthrough,
    DropdownMenuSub: Passthrough,
    DropdownMenuSubTrigger: Passthrough,
    DropdownMenuSubContent: Passthrough,
    DropdownMenuRadioGroup: Passthrough,
  };
});

import { AlertActionMenu } from "./alert-action-menu";

// Stub the network helper so tests don't issue real fetches.
jest.mock("@/lib/api", () => ({
  apiPost: jest.fn().mockResolvedValue({}),
}));

// Stub sonner to avoid mounting the global toaster.
jest.mock("sonner", () => ({
  toast: {
    success: jest.fn(),
    error: jest.fn(),
  },
}));

const ALERT = {
  id: "alert-error-rate-high",
  name: "High Error Rate",
};

function setup(overrides: Partial<React.ComponentProps<typeof AlertActionMenu>> = {}) {
  const onMute = overrides.onMute ?? jest.fn();
  render(
    <AlertActionMenu
      alert={ALERT}
      onMute={onMute}
      isMuted={overrides.isMuted ?? false}
    />,
  );
  return { onMute };
}

describe("AlertActionMenu", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("renders a trigger with an accessible label scoped to the alert", () => {
    setup();
    expect(
      screen.getByRole("button", { name: /Alert actions for High Error Rate/i }),
    ).toBeInTheDocument();
  });

  it("renders all four actions when not muted", async () => {
    setup();
    fireEvent.click(
      screen.getByRole("button", { name: /Alert actions for High Error Rate/i }),
    );

    expect(await screen.findByText(/Acknowledge/)).toBeInTheDocument();
    expect(screen.getByText(/Mute for 1 hour/)).toBeInTheDocument();
    expect(screen.getByText(/Open in new tab/)).toBeInTheDocument();
    expect(screen.getByText(/Copy alert ID/)).toBeInTheDocument();
  });

  it("hides the Mute action when the alert is already muted", async () => {
    setup({ isMuted: true });
    fireEvent.click(
      screen.getByRole("button", { name: /Alert actions for High Error Rate/i }),
    );
    expect(await screen.findByText(/Acknowledge/)).toBeInTheDocument();
    expect(screen.queryByText(/Mute for 1 hour/)).not.toBeInTheDocument();
  });

  it('"Mute for 1 hour" calls onMute with a 1-hour duration', async () => {
    const { onMute } = setup();
    fireEvent.click(
      screen.getByRole("button", { name: /Alert actions for High Error Rate/i }),
    );
    fireEvent.click(await screen.findByText(/Mute for 1 hour/));

    expect(onMute).toHaveBeenCalledTimes(1);
    expect(onMute).toHaveBeenCalledWith("alert-error-rate-high", 60 * 60 * 1000);
  });

  it('"Copy alert ID" writes the ID to the clipboard', async () => {
    const writeText = jest.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    setup();
    fireEvent.click(
      screen.getByRole("button", { name: /Alert actions for High Error Rate/i }),
    );
    fireEvent.click(await screen.findByText(/Copy alert ID/));

    expect(writeText).toHaveBeenCalledWith("alert-error-rate-high");
  });

  it('"Open in new tab" renders an anchor with target=_blank + rel=noopener', async () => {
    setup();
    fireEvent.click(
      screen.getByRole("button", { name: /Alert actions for High Error Rate/i }),
    );

    const link = (await screen.findByText(/Open in new tab/)).closest("a");
    expect(link).not.toBeNull();
    expect(link).toHaveAttribute("target", "_blank");
    expect(link!.getAttribute("rel") || "").toMatch(/noopener/);
    expect(link).toHaveAttribute("href", "/observability");
  });
});
