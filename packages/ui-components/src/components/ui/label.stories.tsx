import type { Story } from "@ladle/react";
import { Label } from "./label";

export default {
  title: "UI / Label",
};

/** Default label text. */
export const Default: Story = () => <Label>Username</Label>;

/** Label with a required visual indicator. */
export const Required: Story = () => (
  <Label>
    Email address <span style={{ color: "hsl(0, 84.2%, 60.2%)" }}>*</span>
  </Label>
);

/** Label associated with an input via htmlFor. */
export const WithHtmlFor: Story = () => (
  <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem" }}>
    <Label htmlFor="demo-field">Project name</Label>
    <input
      id="demo-field"
      style={{
        padding: "0.5rem 0.75rem",
        border: "1px solid hsl(214.3, 31.8%, 91.4%)",
        borderRadius: "0.375rem",
        fontSize: "0.875rem",
      }}
    />
  </div>
);

/** Disabled peer styling preview. */
export const DisabledPeer: Story = () => (
  <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem" }}>
    <Label htmlFor="disabled-field" className="peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
      Disabled field
    </Label>
    <input
      id="disabled-field"
      className="peer"
      disabled
      style={{
        padding: "0.5rem 0.75rem",
        border: "1px solid hsl(214.3, 31.8%, 91.4%)",
        borderRadius: "0.375rem",
        fontSize: "0.875rem",
        opacity: 0.5,
      }}
    />
  </div>
);
