import { LayoutDashboard, Layers, Radio, Puzzle, Award, Shield, Activity, Settings, Save, FileText, Network } from "lucide-react"

export const navItems = [
  { title: "Dashboard", href: "/", icon: LayoutDashboard },
  { title: "Pools", href: "/pools", icon: Layers },
  { title: "Listeners", href: "/listeners", icon: Radio },
  { title: "Middleware", href: "/middleware", icon: Puzzle },
  { title: "Certificates", href: "/certificates", icon: Award },
  { title: "WAF", href: "/waf", icon: Shield },
  { title: "Metrics", href: "/metrics", icon: Activity },
  { title: "Logs", href: "/logs", icon: FileText },
  { title: "Cluster", href: "/cluster", icon: Network },
  { title: "Backup", href: "/backup", icon: Save },
  { title: "Settings", href: "/settings", icon: Settings },
]
