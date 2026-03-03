"use client";

import { useEffect, useState } from "react";
import { UsageMeters } from "@/components/billing/usage-meters";
import { CostBreakdown } from "@/components/billing/cost-breakdown";
import { InvoiceTable } from "@/components/billing/invoice-table";
import { PlanSelector } from "@/components/billing/plan-selector";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CreditCard, Receipt, Settings, Loader2 } from "lucide-react";
import { apiGet } from "@/lib/api";
import { useTier } from "@/hooks/use-tier";

interface BillingInfo {
  plan_name: string;
  plan_base: number;
  period_start: string;
  period_end: string;
  grand_total: number;
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString("en-US", {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return iso;
  }
}

function nextInvoiceDate(periodEnd: string): string {
  try {
    const d = new Date(periodEnd);
    d.setDate(d.getDate() + 1);
    return d.toLocaleDateString("en-US", {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return "—";
  }
}

export default function BillingPage() {
  const { tierName, config } = useTier();
  const [billing, setBilling] = useState<BillingInfo | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    apiGet<BillingInfo>("/v1/usage")
      .then(setBilling)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const planName = billing?.plan_name ?? tierName ?? "Community";
  const planPrice = config?.price ?? "Free";
  const periodLabel =
    billing?.period_start && billing?.period_end
      ? `${formatDate(billing.period_start)} - ${formatDate(billing.period_end)}`
      : "Current period";
  const nextInvoice =
    billing?.period_end ? nextInvoiceDate(billing.period_end) : "—";

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Billing</h1>
          <p className="text-muted-foreground">
            Manage your subscription and view usage
          </p>
        </div>
        <Button variant="outline">
          <CreditCard className="h-4 w-4 mr-2" />
          Payment Methods
        </Button>
      </div>

      <Tabs defaultValue="overview" className="space-y-6">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="usage">Usage</TabsTrigger>
          <TabsTrigger value="invoices">Invoices</TabsTrigger>
          <TabsTrigger value="plan">Plan</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-6">
          {/* Current Plan Summary */}
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                {loading ? (
                  <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                ) : (
                  <>
                    <CardTitle>{planName} Plan</CardTitle>
                    <p className="text-sm text-muted-foreground">
                      {planPrice}/month + usage
                    </p>
                  </>
                )}
              </div>
              <Button variant="outline" size="sm">
                <Settings className="h-4 w-4 mr-2" />
                Manage Plan
              </Button>
            </CardHeader>
            <CardContent>
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-1">
                  <p className="text-sm text-muted-foreground">Billing Period</p>
                  <p className="font-medium">{periodLabel}</p>
                </div>
                <div className="space-y-1">
                  <p className="text-sm text-muted-foreground">Next Invoice</p>
                  <p className="font-medium">{nextInvoice}</p>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Usage and Cost Side by Side */}
          <div className="grid gap-6 md:grid-cols-2">
            <UsageMeters projectId="current" />
            <CostBreakdown projectId="current" />
          </div>

          {/* Recent Invoices */}
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle className="text-base">Recent Invoices</CardTitle>
              <Button variant="ghost" size="sm">
                <Receipt className="h-4 w-4 mr-2" />
                View All
              </Button>
            </CardHeader>
            <CardContent>
              <InvoiceTable invoices={[]} />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="usage" className="space-y-6">
          <UsageMeters projectId="current" className="max-w-2xl" />

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Usage History</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-muted-foreground text-sm">
                Detailed usage charts and history coming soon.
              </p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="invoices" className="space-y-6">
          <InvoiceTable invoices={[]} />
        </TabsContent>

        <TabsContent value="plan" className="space-y-6">
          <PlanSelector currentPlanId={tierName?.toLowerCase() ?? "community"} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
