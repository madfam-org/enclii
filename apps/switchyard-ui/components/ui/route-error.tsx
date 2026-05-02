"use client";

import { useEffect } from "react";
import { AlertTriangle, RefreshCw } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@enclii/ui-components/button";

interface RouteErrorProps {
  error: Error & { digest?: string };
  reset: () => void;
  title?: string;
}

export function RouteError({ error, reset, title = "Something went wrong" }: RouteErrorProps) {
  useEffect(() => {
    // Log the error to an error reporting service like Sentry
    console.error("Route error caught by boundary:", error);
  }, [error]);

  return (
    <div className="container mx-auto py-8 h-[50vh] flex flex-col items-center justify-center">
      <Card className="max-w-md w-full border-status-error/30 bg-status-error-muted shadow-sm">
        <CardContent className="pt-6 pb-8 text-center flex flex-col items-center">
          <div className="h-12 w-12 rounded-full bg-status-error/20 flex items-center justify-center mb-4">
            <AlertTriangle className="h-6 w-6 text-status-error" />
          </div>
          
          <h2 className="text-xl font-semibold text-foreground mb-2">
            {title}
          </h2>
          
          <p className="text-sm text-muted-foreground mb-6 line-clamp-3">
            {error.message || "An unexpected error occurred while loading this page."}
          </p>

          <Button 
            onClick={() => reset()} 
            className="w-full sm:w-auto"
            variant="default"
          >
            <RefreshCw className="mr-2 h-4 w-4" />
            Try again
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
