"use client";

import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import {
  ArrowRight,
  ArrowLeft,
  Check,
  Rocket,
  FolderPlus,
  User
} from "lucide-react";
import { cn } from "@/lib/utils";

interface OnboardingWizardProps {
  onComplete?: (data: OnboardingData) => void;
}

interface OnboardingData {
  profile: {
    name: string;
    company?: string;
  };
  project: {
    name: string;
    slug: string;
  };
}

const steps = [
  { id: "profile", title: "Your Profile", icon: User },
  { id: "project", title: "First Project", icon: FolderPlus },
  { id: "complete", title: "Ready!", icon: Rocket },
];

export function OnboardingWizard({ onComplete }: OnboardingWizardProps) {
  const [currentStep, setCurrentStep] = useState(0);
  const [data, setData] = useState<OnboardingData>({
    profile: { name: "" },
    project: { name: "", slug: "" },
  });

  const progress = ((currentStep + 1) / steps.length) * 100;

  const handleNext = () => {
    if (currentStep < steps.length - 1) {
      setCurrentStep(currentStep + 1);
    } else {
      onComplete?.(data);
    }
  };

  const handleBack = () => {
    if (currentStep > 0) {
      setCurrentStep(currentStep - 1);
    }
  };

  const generateSlug = (name: string) => {
    return name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "");
  };

  const updateProject = (name: string) => {
    setData({
      ...data,
      project: {
        name,
        slug: generateSlug(name),
      },
    });
  };

  const canProceed = () => {
    switch (steps[currentStep].id) {
      case "profile":
        return data.profile.name.length >= 2;
      case "project":
        return data.project.name.length >= 2;
      default:
        return true;
    }
  };

  return (
    <div className="max-w-2xl mx-auto">
      {/* Progress Header */}
      <div className="mb-8">
        <div className="flex items-center justify-between mb-4">
          {steps.map((step, index) => {
            const Icon = step.icon;
            const isCompleted = index < currentStep;
            const isCurrent = index === currentStep;

            return (
              <div
                key={step.id}
                className="flex items-center"
              >
                <div className={cn(
                  "flex items-center justify-center w-10 h-10 rounded-full border-2 transition-colors",
                  isCompleted && "bg-primary border-primary text-primary-foreground",
                  isCurrent && "border-primary text-primary",
                  !isCompleted && !isCurrent && "border-muted text-muted-foreground"
                )}>
                  {isCompleted ? (
                    <Check className="h-5 w-5" />
                  ) : (
                    <Icon className="h-5 w-5" />
                  )}
                </div>
                {index < steps.length - 1 && (
                  <div className={cn(
                    "w-16 h-0.5 mx-2",
                    index < currentStep ? "bg-primary" : "bg-muted"
                  )} />
                )}
              </div>
            );
          })}
        </div>
        <Progress value={progress} className="h-1" />
      </div>

      {/* Step Content */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            {(() => {
              const Icon = steps[currentStep].icon;
              return <Icon className="h-5 w-5" />;
            })()}
            {steps[currentStep].title}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* Profile Step */}
          {steps[currentStep].id === "profile" && (
            <div className="space-y-4">
              <p className="text-muted-foreground">
                Let's get to know you better
              </p>
              <div className="space-y-2">
                <Label htmlFor="name">Your Name</Label>
                <Input
                  id="name"
                  placeholder="John Doe"
                  value={data.profile.name}
                  onChange={(e) => setData({
                    ...data,
                    profile: { ...data.profile, name: e.target.value }
                  })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="company">Company (optional)</Label>
                <Input
                  id="company"
                  placeholder="Acme Inc."
                  value={data.profile.company || ""}
                  onChange={(e) => setData({
                    ...data,
                    profile: { ...data.profile, company: e.target.value }
                  })}
                />
              </div>
            </div>
          )}

          {/* Project Step */}
          {steps[currentStep].id === "project" && (
            <div className="space-y-4">
              <p className="text-muted-foreground">
                Create your first project to get started
              </p>
              <div className="space-y-2">
                <Label htmlFor="projectName">Project Name</Label>
                <Input
                  id="projectName"
                  placeholder="My Awesome App"
                  value={data.project.name}
                  onChange={(e) => updateProject(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="projectSlug">Project URL</Label>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">enclii.dev/</span>
                  <Input
                    id="projectSlug"
                    value={data.project.slug}
                    onChange={(e) => setData({
                      ...data,
                      project: { ...data.project, slug: e.target.value }
                    })}
                  />
                </div>
              </div>
            </div>
          )}

          {/* Complete Step */}
          {steps[currentStep].id === "complete" && (
            <div className="text-center space-y-4 py-8">
              <div className="w-16 h-16 mx-auto bg-status-success-muted rounded-full flex items-center justify-center">
                <Rocket className="h-8 w-8 text-status-success" />
              </div>
              <h3 className="text-xl font-semibold">You're all set!</h3>
              <p className="text-muted-foreground max-w-md mx-auto">
                Your account is ready. Let's deploy your first service and see
                Enclii in action.
              </p>
              <div className="bg-muted p-4 rounded-lg text-left">
                <p className="text-sm font-medium mb-2">Summary:</p>
                <ul className="text-sm text-muted-foreground space-y-1">
                  <li>• Project: {data.project.name}</li>
                  <li>• URL: enclii.dev/{data.project.slug}</li>
                </ul>
              </div>
            </div>
          )}

          {/* Navigation */}
          <div className="flex items-center justify-between pt-4 border-t">
            <Button
              variant="ghost"
              onClick={handleBack}
              disabled={currentStep === 0}
            >
              <ArrowLeft className="h-4 w-4 mr-2" />
              Back
            </Button>
            <Button
              onClick={handleNext}
              disabled={!canProceed()}
            >
              {currentStep === steps.length - 1 ? (
                <>
                  Go to Dashboard
                  <Rocket className="h-4 w-4 ml-2" />
                </>
              ) : (
                <>
                  Continue
                  <ArrowRight className="h-4 w-4 ml-2" />
                </>
              )}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
