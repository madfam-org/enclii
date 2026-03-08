import type { Story } from "@ladle/react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./dialog";
import { Button } from "./button";
import { Input } from "./input";
import { Label } from "./label";

export default {
  title: "UI / Dialog",
};

/** Basic dialog with a title and description. */
export const Default: Story = () => (
  <Dialog>
    <DialogTrigger asChild>
      <Button variant="outline">Open Dialog</Button>
    </DialogTrigger>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Are you sure?</DialogTitle>
        <DialogDescription>
          This action cannot be undone. This will permanently delete your
          service and remove all associated data.
        </DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <Button variant="outline">Cancel</Button>
        <Button variant="destructive">Delete</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
);

/** Dialog containing a form with labelled inputs. */
export const WithForm: Story = () => (
  <Dialog>
    <DialogTrigger asChild>
      <Button>Create Service</Button>
    </DialogTrigger>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>New Service</DialogTitle>
        <DialogDescription>
          Configure your new service. You can change these settings later.
        </DialogDescription>
      </DialogHeader>
      <div style={{ display: "flex", flexDirection: "column", gap: "1rem" }}>
        <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem" }}>
          <Label htmlFor="service-name">Service name</Label>
          <Input id="service-name" placeholder="my-api" />
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem" }}>
          <Label htmlFor="service-port">Port</Label>
          <Input id="service-port" type="number" placeholder="4200" />
        </div>
      </div>
      <DialogFooter>
        <Button variant="outline">Cancel</Button>
        <Button>Create</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
);
