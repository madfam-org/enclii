import type { Story } from "@ladle/react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "./dropdown-menu";
import { Button } from "./button";

export default {
  title: "UI / DropdownMenu",
};

/** Basic dropdown with simple menu items. */
export const Default: Story = () => (
  <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <Button variant="outline">Open Menu</Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent style={{ minWidth: "10rem" }}>
      <DropdownMenuLabel>Actions</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuItem>View Details</DropdownMenuItem>
      <DropdownMenuItem>Edit Service</DropdownMenuItem>
      <DropdownMenuItem>Duplicate</DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem>Delete</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
);

/** Dropdown with keyboard shortcuts displayed. */
export const WithShortcuts: Story = () => (
  <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <Button variant="outline">File</Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent style={{ minWidth: "12rem" }}>
      <DropdownMenuGroup>
        <DropdownMenuItem>
          New Service
          <DropdownMenuShortcut>Ctrl+N</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem>
          Open Project
          <DropdownMenuShortcut>Ctrl+O</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem>
          Save
          <DropdownMenuShortcut>Ctrl+S</DropdownMenuShortcut>
        </DropdownMenuItem>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuItem disabled>
        Export
        <DropdownMenuShortcut>Ctrl+E</DropdownMenuShortcut>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
);

/** Dropdown with a nested sub-menu. */
export const WithSubMenu: Story = () => (
  <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <Button variant="outline">Options</Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent style={{ minWidth: "12rem" }}>
      <DropdownMenuLabel>Environment</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuItem>Production</DropdownMenuItem>
      <DropdownMenuItem>Staging</DropdownMenuItem>
      <DropdownMenuSub>
        <DropdownMenuSubTrigger>More Environments</DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          <DropdownMenuItem>Development</DropdownMenuItem>
          <DropdownMenuItem>Preview</DropdownMenuItem>
          <DropdownMenuItem>Canary</DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>
      <DropdownMenuSeparator />
      <DropdownMenuItem>Manage Environments</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
);
