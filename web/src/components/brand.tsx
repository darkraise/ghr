export function Brand({ className = "" }: { className?: string }) {
  return (
    <div className={`flex items-center justify-center gap-2 ${className}`}>
      <svg width="28" height="28" viewBox="0 0 22 22" aria-hidden="true" className="text-foreground">
        <rect x="2" y="3.5" width="18" height="3.2" rx="1.6" fill="currentColor" />
        <rect x="2" y="9.4" width="12" height="3.2" rx="1.6" fill="currentColor" opacity="0.62" />
        <rect x="2" y="15.3" width="6.5" height="3.2" rx="1.6" fill="currentColor" opacity="0.38" />
        <circle cx="16.6" cy="16.9" r="2.6" className="fill-primary" />
      </svg>
      <span className="font-mono text-xl font-semibold">ghr</span>
    </div>
  )
}
