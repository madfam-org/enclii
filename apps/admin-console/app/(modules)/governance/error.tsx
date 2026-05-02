"use client";
import { AlertTriangle, RefreshCw } from "lucide-react";
export default function Error({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4 text-center">
      <div className="h-12 w-12 rounded-full bg-destructive/20 flex items-center justify-center">
        <AlertTriangle className="h-6 w-6 text-destructive" />
      </div>
      <p className="font-medium">{error.message || "Failed to load module"}</p>
      <button onClick={reset} className="flex items-center gap-2 px-4 py-2 text-sm rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors">
        <RefreshCw className="h-4 w-4" /> Retry
      </button>
    </div>
  );
}
