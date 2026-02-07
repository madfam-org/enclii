"use client";

import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Check, Zap } from "lucide-react";
import { cn } from "@/lib/utils";

interface Plan {
  id: string;
  name: string;
  description: string;
  priceMonthly: number;
  includes: {
    computeGbHours: number;
    buildMinutes: number;
    storageGb: number;
    bandwidthGb: number;
    customDomains: number | "unlimited";
    teamMembers?: number;
  };
  features: string[];
  popular?: boolean;
}

const plans: Plan[] = [
  {
    id: "hobby",
    name: "Hobby",
    description: "For personal projects and experiments",
    priceMonthly: 5,
    includes: {
      computeGbHours: 500,
      buildMinutes: 500,
      storageGb: 1,
      bandwidthGb: 100,
      customDomains: 1,
    },
    features: [
      "1 custom domain",
      "Community support",
      "Basic monitoring",
    ],
  },
  {
    id: "pro",
    name: "Pro",
    description: "For production applications",
    priceMonthly: 20,
    includes: {
      computeGbHours: 2000,
      buildMinutes: 2000,
      storageGb: 10,
      bandwidthGb: 500,
      customDomains: "unlimited",
    },
    features: [
      "Unlimited custom domains",
      "Priority support",
      "Team collaboration",
      "Advanced metrics",
      "Custom health checks",
    ],
    popular: true,
  },
  {
    id: "team",
    name: "Team",
    description: "For teams and organizations",
    priceMonthly: 50,
    includes: {
      computeGbHours: 5000,
      buildMinutes: 5000,
      storageGb: 50,
      bandwidthGb: 1000,
      customDomains: "unlimited",
      teamMembers: 10,
    },
    features: [
      "Everything in Pro",
      "SSO/SAML",
      "Audit logs",
      "SLA guarantee",
      "Dedicated support",
      "10 team members included",
    ],
  },
];

interface PlanSelectorProps {
  currentPlanId?: string;
  onSelectPlan?: (planId: string) => void;
  className?: string;
}

export function PlanSelector({
  currentPlanId = "hobby",
  onSelectPlan,
  className
}: PlanSelectorProps) {
  const [selectedPlan, setSelectedPlan] = useState(currentPlanId);

  const handleSelect = (planId: string) => {
    setSelectedPlan(planId);
    onSelectPlan?.(planId);
  };

  return (
    <div className={cn("grid gap-4 md:grid-cols-3", className)}>
      {plans.map((plan) => {
        const isSelected = selectedPlan === plan.id;
        const isCurrent = currentPlanId === plan.id;

        return (
          <Card
            key={plan.id}
            className={cn(
              "relative cursor-pointer transition-all",
              isSelected && "ring-2 ring-primary",
              plan.popular && "border-primary"
            )}
            onClick={() => handleSelect(plan.id)}
          >
            {plan.popular && (
              <Badge
                className="absolute -top-2 left-1/2 -translate-x-1/2"
                variant="default"
              >
                <Zap className="h-3 w-3 mr-1" />
                Most Popular
              </Badge>
            )}

            <CardHeader className="pb-2">
              <div className="flex items-center justify-between">
                <CardTitle className="text-lg">{plan.name}</CardTitle>
                {isCurrent && (
                  <Badge variant="secondary">Current</Badge>
                )}
              </div>
              <p className="text-sm text-muted-foreground">
                {plan.description}
              </p>
            </CardHeader>

            <CardContent className="space-y-4">
              <div>
                <span className="text-3xl font-bold">
                  ${plan.priceMonthly}
                </span>
                <span className="text-muted-foreground">/month</span>
              </div>

              <div className="space-y-2 text-sm">
                <p className="font-medium">Includes:</p>
                <ul className="space-y-1 text-muted-foreground">
                  <li>{plan.includes.computeGbHours.toLocaleString()} GB-hours compute</li>
                  <li>{plan.includes.buildMinutes.toLocaleString()} build minutes</li>
                  <li>{plan.includes.storageGb} GB storage</li>
                  <li>{plan.includes.bandwidthGb.toLocaleString()} GB bandwidth</li>
                </ul>
              </div>

              <div className="space-y-2 pt-2 border-t">
                {plan.features.map((feature) => (
                  <div key={feature} className="flex items-center gap-2 text-sm">
                    <Check className="h-4 w-4 text-status-success" />
                    <span>{feature}</span>
                  </div>
                ))}
              </div>

              <Button
                className="w-full"
                variant={isSelected ? "default" : "outline"}
                disabled={isCurrent}
              >
                {isCurrent ? "Current Plan" : isSelected ? "Selected" : "Select Plan"}
              </Button>
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}
