import { describe, it, expect } from 'vitest'
import { render, screen } from '@/test/utils'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'

describe('Card', () => {
  it('renders children', () => {
    render(
      <Card>
        <CardHeader>
          <CardTitle>Test Title</CardTitle>
          <CardDescription>Test description</CardDescription>
        </CardHeader>
        <CardContent>Card body content</CardContent>
      </Card>,
    )

    expect(screen.getByText('Test Title')).toBeInTheDocument()
    expect(screen.getByText('Test description')).toBeInTheDocument()
    expect(screen.getByText('Card body content')).toBeInTheDocument()
  })

  it('applies custom className', () => {
    render(<Card className="custom-card">Content</Card>)
    expect(screen.getByText('Content').className).toContain('custom-card')
  })
})
