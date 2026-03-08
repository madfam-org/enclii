import type { Story } from "@ladle/react";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "./select";
import { Label } from "./label";

export default {
  title: "UI / Select",
};

/** Basic select with a few options. */
export const Default: Story = () => (
  <Select>
    <SelectTrigger style={{ width: "14rem" }}>
      <SelectValue placeholder="Select a region" />
    </SelectTrigger>
    <SelectContent>
      <SelectItem value="us-east">US East</SelectItem>
      <SelectItem value="us-west">US West</SelectItem>
      <SelectItem value="eu-central">EU Central</SelectItem>
      <SelectItem value="ap-south">AP South</SelectItem>
    </SelectContent>
  </Select>
);

/** Select with labelled groups and separators. */
export const WithGroups: Story = () => (
  <Select>
    <SelectTrigger style={{ width: "14rem" }}>
      <SelectValue placeholder="Select environment" />
    </SelectTrigger>
    <SelectContent>
      <SelectGroup>
        <SelectLabel>Active</SelectLabel>
        <SelectItem value="production">Production</SelectItem>
        <SelectItem value="staging">Staging</SelectItem>
      </SelectGroup>
      <SelectSeparator />
      <SelectGroup>
        <SelectLabel>Preview</SelectLabel>
        <SelectItem value="preview-123">PR #123</SelectItem>
        <SelectItem value="preview-456">PR #456</SelectItem>
      </SelectGroup>
    </SelectContent>
  </Select>
);

/** Select paired with a Label component. */
export const WithLabel: Story = () => (
  <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem" }}>
    <Label htmlFor="runtime-select">Runtime</Label>
    <Select>
      <SelectTrigger id="runtime-select" style={{ width: "14rem" }}>
        <SelectValue placeholder="Choose runtime" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="nodejs">Node.js</SelectItem>
        <SelectItem value="go">Go</SelectItem>
        <SelectItem value="python">Python</SelectItem>
        <SelectItem value="rust">Rust</SelectItem>
      </SelectContent>
    </Select>
  </div>
);

/** Disabled select. */
export const Disabled: Story = () => (
  <Select disabled>
    <SelectTrigger style={{ width: "14rem" }}>
      <SelectValue placeholder="Locked" />
    </SelectTrigger>
    <SelectContent>
      <SelectItem value="locked">Locked option</SelectItem>
    </SelectContent>
  </Select>
);
