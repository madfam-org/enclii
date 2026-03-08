import type { Story } from "@ladle/react";
import { Badge } from "./badge";

export default {
  title: "UI / Badge",
};

/** Default (primary) variant. */
export const Default: Story = () => <Badge>Default</Badge>;

/** Secondary variant. */
export const Secondary: Story = () => (
  <Badge variant="secondary">Secondary</Badge>
);

/** Destructive variant for errors or critical states. */
export const Destructive: Story = () => (
  <Badge variant="destructive">Destructive</Badge>
);

/** Outline variant with border only. */
export const Outline: Story = () => (
  <Badge variant="outline">Outline</Badge>
);

/** Success variant for positive states. */
export const Success: Story = () => (
  <Badge variant="success">Healthy</Badge>
);

/** Warning variant for cautionary states. */
export const Warning: Story = () => (
  <Badge variant="warning">Degraded</Badge>
);

/** Info variant for informational states. */
export const Info: Story = () => <Badge variant="info">Syncing</Badge>;

/** All variants displayed together for comparison. */
export const AllVariants: Story = () => (
  <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
    <Badge variant="default">Default</Badge>
    <Badge variant="secondary">Secondary</Badge>
    <Badge variant="destructive">Destructive</Badge>
    <Badge variant="outline">Outline</Badge>
    <Badge variant="success">Healthy</Badge>
    <Badge variant="warning">Degraded</Badge>
    <Badge variant="info">Syncing</Badge>
  </div>
);
