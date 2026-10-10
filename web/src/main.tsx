import "@fontsource-variable/mona-sans/index.css"
import "@fontsource/jetbrains-mono/latin-400.css"
import "@fontsource/jetbrains-mono/latin-600.css"
import "./styles/globals.css"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./app"
import { createQueryClient } from "./query"
import { createAppRouter } from "./router"

const root = document.getElementById("root")
if (!root) throw new Error("index.html has no #root element")
const queryClient = createQueryClient()
const router = createAppRouter({ queryClient })
createRoot(root).render(
  <StrictMode>
    <App router={router} queryClient={queryClient} />
  </StrictMode>,
)
