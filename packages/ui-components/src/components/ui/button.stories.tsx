import type { Story } from "@ladle/react";
import { Button } from "./button";

export default {
  title: "UI / Button",
};

/** Default (primary) variant. */
export const Default: Story = () => <Button>Click me</Button>;

/** Destructive variant for dangerous actions. */
export const Destructive: Story = () => (
  <Button variant="destructive">Delete</Button>
);

/** Outline variant with transparent background. */
export const Outline: Story = () => (
  <Button variant="outline">Outline</Button>
);

/** Secondary variant with muted styling. */
export const Secondary: Story = () => (
  <Button variant="secondary">Secondary</Button>
);

/** Ghost variant -- no border or background until hover. */
export const Ghost: Story = () => <Button variant="ghost">Ghost</Button>;

/** Link variant renders as an inline text link. */
export const Link: Story = () => <Button variant="link">Link</Button>;

/** Small size. */
export const SizeSmall: Story = () => (
  <Button size="sm">Small</Button>
);

/** Large size. */
export const SizeLarge: Story = () => (
  <Button size="lg">Large</Button>
);

/** Icon-only size (square). */
export const SizeIcon: Story = () => (
  <Button size="icon" aria-label="Settings">
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  </Button>
);

/** Disabled state. */
export const Disabled: Story = () => (
  <Button disabled>Disabled</Button>
);

/** Button with a leading icon. */
export const WithIcon: Story = () => (
  <Button>
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      style={{ marginRight: "0.5rem" }}
    >
      <path d="M5 12h14" />
      <path d="M12 5v14" />
    </svg>
    Add item
  </Button>
);

/** All variants displayed together for comparison. */
export const AllVariants: Story = () => (
  <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
    <Button variant="default">Default</Button>
    <Button variant="destructive">Destructive</Button>
    <Button variant="outline">Outline</Button>
    <Button variant="secondary">Secondary</Button>
    <Button variant="ghost">Ghost</Button>
    <Button variant="link">Link</Button>
  </div>
);
