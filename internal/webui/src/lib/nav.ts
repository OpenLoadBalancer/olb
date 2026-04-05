import { LayoutDashboard, Layers, Radio, Settings, Shield, Activity, Puzzle, Award } from "lucide-react"

export const navItems = [
  { title: "Dashboard", href: "/", icon: LayoutDashboard },
  { title: "Pools", href: "/pools", icon: Layers },
  { title: "Listeners", href: "/listeners", icon: Radio },
  { title: "Middleware", href: "/middleware", icon: Puzzle },
  { title: "Certificates", href: "/certificates", icon: Award },
  { title: "WAF", href: "/waf", icon: Shield },
  { title: "Metrics", href: "/metrics", icon: Activity },
  { title: "Settings", href: "/settings", icon: Settings },
]
