export function Glyph({ symbol, label, className }: { symbol: string; label: string; className?: string }) {
  return (
    <span role="img" aria-label={label} className={className}>
      {symbol}
    </span>
  )
}
