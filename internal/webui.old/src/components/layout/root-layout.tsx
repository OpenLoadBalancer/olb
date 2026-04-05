import { Outlet } from 'react-router'
import { Sidebar } from './sidebar'
import { Header } from './header'
import { GlobalSearch } from '../global-search'

export function RootLayout() {
  return (
    <div className="flex h-screen w-full overflow-hidden bg-background">
      <Sidebar />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
      <GlobalSearch />
    </div>
  )
}
