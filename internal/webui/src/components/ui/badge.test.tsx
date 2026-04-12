import { describe, it, expect } from 'vitest'
import { render, screen } from '@/test/utils'
import { Badge } from '@/components/ui/badge'

describe('Badge', () => {
  it('renders text content', () => {
    render(<Badge>Active</Badge>)
    expect(screen.getByText('Active')).toBeInTheDocument()
  })

  it('applies default variant classes', () => {
    render(<Badge>Status</Badge>)
    const badge = screen.getByText('Status')
    expect(badge.className).toContain('bg-primary')
  })

  it('applies destructive variant classes', () => {
    render(<Badge variant="destructive">Error</Badge>)
    const badge = screen.getByText('Error')
    expect(badge.className).toContain('bg-destructive')
  })

  it('applies outline variant classes', () => {
    render(<Badge variant="outline">Info</Badge>)
    const badge = screen.getByText('Info')
    expect(badge.className).toContain('text-foreground')
  })

  it('applies custom className', () => {
    render(<Badge className="custom-class">Test</Badge>)
    expect(screen.getByText('Test').className).toContain('custom-class')
  })
})
