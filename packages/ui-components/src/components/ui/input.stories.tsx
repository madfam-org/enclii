import type { Story } from "@ladle/react";
import { Input } from "./input";
import { Label } from "./label";

export default {
  title: "UI / Input",
};

/** Default empty input. */
export const Default: Story = () => <Input />;

/** Input with placeholder text. */
export const WithPlaceholder: Story = () => (
  <Input placeholder="Enter your email" />
);

/** Disabled input. */
export const Disabled: Story = () => (
  <Input placeholder="Cannot edit" disabled />
);

/** Input paired with a Label. */
export const WithLabel: Story = () => (
  <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem", maxWidth: "20rem" }}>
    <Label htmlFor="email-input">Email address</Label>
    <Input id="email-input" type="email" placeholder="you@example.com" />
  </div>
);

/** Password input type. */
export const Password: Story = () => (
  <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem", maxWidth: "20rem" }}>
    <Label htmlFor="password-input">Password</Label>
    <Input id="password-input" type="password" placeholder="Enter password" />
  </div>
);

/** File upload input. */
export const File: Story = () => (
  <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem", maxWidth: "20rem" }}>
    <Label htmlFor="file-input">Upload file</Label>
    <Input id="file-input" type="file" />
  </div>
);
