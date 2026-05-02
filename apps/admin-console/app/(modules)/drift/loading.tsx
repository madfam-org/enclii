export default function Loading() {
  return (
    <div className="animate-pulse space-y-4" aria-busy="true">
      <div className="h-6 w-40 bg-muted rounded" />
      <div className="h-4 w-64 bg-muted/60 rounded" />
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mt-6">
        {[0,1,2].map(i => <div key={i} className="h-24 bg-muted rounded-lg" />)}
      </div>
      <div className="space-y-3 mt-2">
        {[0,1,2,3].map(i => <div key={i} className="h-14 bg-muted/60 rounded-lg" />)}
      </div>
    </div>
  );
}
