import { describe, it, expect } from 'vitest'
import { render, screen } from '@/test/utils'
import { Button } from '@/components/ui/button'

describe('Button', () => {
  it('renders with default variant', () => {
    render(<Button>Click me</Button>)
    const button = screen.getByRole('button', { name: 'Click me' })
    expect(button).toBeInTheDocument()
  })

  it('renders with custom variant classes', () => {
    render(<Button variant="destructive">Delete</Button>)
    const button = screen.getByRole('button', { name: 'Delete' })
    expect(button).toBeInTheDocument()
    expect(button.className).toContain('bg-destructive')
  })

  it('renders with custom size classes', () => {
    render(<Button size="sm">Small</Button>)
    const button = screen.getByRole('button', { name: 'Small' })
    expect(button.className).toContain('h-8')
  })

  it('is disabled when disabled prop is set', () => {
    render(<Button disabled>Disabled</Button>)
    const button = screen.getByRole('button', { name: 'Disabled' })
    expect(button).toBeDisabled()
  })

  it('applies custom className', () => {
    render(<Button className="my-custom-class">Custom</Button>)
    const button = screen.getByRole('button', { name: 'Custom' })
    expect(button.className).toContain('my-custom-class')
  })

  it('handles click events', async () => {
    let clicked = false
    render(<Button onClick={() => { clicked = true }}>Click</Button>)
    const button = screen.getByRole('button', { name: 'Click' })
    button.click()
    expect(clicked).toBe(true)
  })
})
