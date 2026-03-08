import type { Story } from "@ladle/react";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TableFooter,
} from "./table";
import { Badge } from "./badge";

export default {
  title: "UI / Table",
};

const services = [
  { name: "switchyard-api", port: 4200, status: "Healthy", replicas: "2/2" },
  { name: "switchyard-ui", port: 4201, status: "Healthy", replicas: "2/2" },
  { name: "dispatch", port: 4203, status: "Healthy", replicas: "1/1" },
  { name: "status-page", port: 4204, status: "Degraded", replicas: "1/2" },
  { name: "roundhouse", port: null, status: "Stopped", replicas: "0/1" },
];

/** Basic table with headers and data rows. */
export const Default: Story = () => (
  <Table>
    <TableHeader>
      <TableRow>
        <TableHead>Service</TableHead>
        <TableHead>Port</TableHead>
        <TableHead>Status</TableHead>
        <TableHead style={{ textAlign: "right" }}>Replicas</TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {services.map((svc) => (
        <TableRow key={svc.name}>
          <TableCell style={{ fontWeight: 500 }}>{svc.name}</TableCell>
          <TableCell>{svc.port ?? "-"}</TableCell>
          <TableCell>{svc.status}</TableCell>
          <TableCell style={{ textAlign: "right" }}>{svc.replicas}</TableCell>
        </TableRow>
      ))}
    </TableBody>
  </Table>
);

/** Table with a caption. */
export const WithCaption: Story = () => (
  <Table>
    <TableCaption>Enclii production services (port block 4200-4299)</TableCaption>
    <TableHeader>
      <TableRow>
        <TableHead>Service</TableHead>
        <TableHead>Port</TableHead>
        <TableHead>Status</TableHead>
        <TableHead style={{ textAlign: "right" }}>Replicas</TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {services.map((svc) => (
        <TableRow key={svc.name}>
          <TableCell style={{ fontWeight: 500 }}>{svc.name}</TableCell>
          <TableCell>{svc.port ?? "-"}</TableCell>
          <TableCell>{svc.status}</TableCell>
          <TableCell style={{ textAlign: "right" }}>{svc.replicas}</TableCell>
        </TableRow>
      ))}
    </TableBody>
  </Table>
);

/** Table with Badge components for status and a footer row. */
export const WithBadgesAndFooter: Story = () => {
  const statusVariant = (status: string) => {
    switch (status) {
      case "Healthy":
        return "success" as const;
      case "Degraded":
        return "warning" as const;
      case "Stopped":
        return "destructive" as const;
      default:
        return "secondary" as const;
    }
  };

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Service</TableHead>
          <TableHead>Port</TableHead>
          <TableHead>Status</TableHead>
          <TableHead style={{ textAlign: "right" }}>Replicas</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {services.map((svc) => (
          <TableRow key={svc.name}>
            <TableCell style={{ fontWeight: 500 }}>{svc.name}</TableCell>
            <TableCell>{svc.port ?? "-"}</TableCell>
            <TableCell>
              <Badge variant={statusVariant(svc.status)}>{svc.status}</Badge>
            </TableCell>
            <TableCell style={{ textAlign: "right" }}>{svc.replicas}</TableCell>
          </TableRow>
        ))}
      </TableBody>
      <TableFooter>
        <TableRow>
          <TableCell colSpan={3}>Total Services</TableCell>
          <TableCell style={{ textAlign: "right" }}>{services.length}</TableCell>
        </TableRow>
      </TableFooter>
    </Table>
  );
};
