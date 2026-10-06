import { ThemeProvider } from "darkraise-ui/theme"
import { themeConfig } from "./theme.config"

export function App() {
  return (
    <ThemeProvider config={themeConfig}>
      <main className="flex min-h-screen items-center justify-center">
        <h1 className="text-2xl font-medium">ghr</h1>
      </main>
    </ThemeProvider>
  )
}
