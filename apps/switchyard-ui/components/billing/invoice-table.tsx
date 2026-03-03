"use client";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Download, ExternalLink } from "lucide-react";
import { cn } from "@/lib/utils";

interface Invoice {
  id: string;
  number: string;
  date: string;
  dueDate: string;
  amount: number;
  status: "paid" | "pending" | "overdue" | "draft";
  pdfUrl?: string;
  stripeUrl?: string;
}

interface InvoiceTableProps {
  invoices: Invoice[];
  className?: string;
}

const statusConfig = {
  paid: { label: "Paid", variant: "default" as const },
  pending: { label: "Pending", variant: "secondary" as const },
  overdue: { label: "Overdue", variant: "destructive" as const },
  draft: { label: "Draft", variant: "outline" as const },
};

export function InvoiceTable({
  invoices,
  className
}: InvoiceTableProps) {
  return (
    <div className={cn("rounded-md border", className)}>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Invoice</TableHead>
            <TableHead>Date</TableHead>
            <TableHead>Due Date</TableHead>
            <TableHead>Amount</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {invoices.length === 0 ? (
            <TableRow>
              <TableCell colSpan={6} className="text-center py-8 text-muted-foreground">
                No invoices yet
              </TableCell>
            </TableRow>
          ) : (
            invoices.map((invoice) => (
              <TableRow key={invoice.id}>
                <TableCell className="font-medium font-mono">
                  {invoice.number}
                </TableCell>
                <TableCell>{invoice.date}</TableCell>
                <TableCell>{invoice.dueDate}</TableCell>
                <TableCell className="font-mono">
                  ${invoice.amount.toFixed(2)}
                </TableCell>
                <TableCell>
                  <Badge variant={statusConfig[invoice.status].variant}>
                    {statusConfig[invoice.status].label}
                  </Badge>
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    {invoice.pdfUrl && (
                      <Button variant="ghost" size="icon" className="h-8 w-8" asChild>
                        <a href={invoice.pdfUrl} download>
                          <Download className="h-4 w-4" />
                        </a>
                      </Button>
                    )}
                    {invoice.stripeUrl && (
                      <Button variant="ghost" size="icon" className="h-8 w-8" asChild>
                        <a href={invoice.stripeUrl} target="_blank" rel="noopener noreferrer">
                          <ExternalLink className="h-4 w-4" />
                        </a>
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
