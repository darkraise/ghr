import "@fontsource/inter/400.css"
import "@fontsource/inter/500.css"
import "@fontsource/inter/600.css"
import "@fontsource/inter/700.css"
import "@fontsource/jetbrains-mono/400.css"
import "@fontsource/jetbrains-mono/500.css"
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
