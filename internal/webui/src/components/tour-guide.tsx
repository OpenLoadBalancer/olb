import { useState, useEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { ChevronRight, ChevronLeft, X } from 'lucide-react'
import { cn } from '@/lib/utils'

interface TourStep {
  target: string
  title: string
  content: string
  placement?: 'top' | 'bottom' | 'left' | 'right'
}

interface TourGuideProps {
  steps: TourStep[]
  isOpen: boolean
  onClose: () => void
  onComplete?: () => void
}

export function TourGuide({ steps, isOpen, onClose, onComplete }: TourGuideProps) {
  const [currentStep, setCurrentStep] = useState(0)
  const [targetRect, setTargetRect] = useState<DOMRect | null>(null)

  const updateTargetRect = useCallback(() => {
    if (!isOpen) return
    const step = steps[currentStep]
    const target = document.querySelector(step.target)
    if (target) {
      setTargetRect(target.getBoundingClientRect())
    }
  }, [currentStep, isOpen, steps])

  useEffect(() => {
    updateTargetRect()
    window.addEventListener('resize', updateTargetRect)
    return () => window.removeEventListener('resize', updateTargetRect)
  }, [updateTargetRect])

  useEffect(() => {
    if (isOpen) {
      setCurrentStep(0)
      updateTargetRect()
    }
  }, [isOpen, updateTargetRect])

  const handleNext = () => {
    if (currentStep < steps.length - 1) {
      setCurrentStep(prev => prev + 1)
    } else {
      onComplete?.()
      onClose()
    }
  }

  const handlePrev = () => {
    if (currentStep > 0) {
      setCurrentStep(prev => prev - 1)
    }
  }

  const handleSkip = () => {
    onClose()
  }

  if (!isOpen || !targetRect) return null

  const step = steps[currentStep]
  const placement = step.placement || 'bottom'

  const getTooltipPosition = () => {
    const offset = 16
    switch (placement) {
      case 'top':
        return {
          left: targetRect.left + targetRect.width / 2,
          top: targetRect.top - offset,
          transform: 'translate(-50%, -100%)'
        }
      case 'bottom':
        return {
          left: targetRect.left + targetRect.width / 2,
          top: targetRect.bottom + offset,
          transform: 'translate(-50%, 0)'
        }
      case 'left':
        return {
          left: targetRect.left - offset,
          top: targetRect.top + targetRect.height / 2,
          transform: 'translate(-100%, -50%)'
        }
      case 'right':
        return {
          left: targetRect.right + offset,
          top: targetRect.top + targetRect.height / 2,
          transform: 'translate(0, -50%)'
        }
    }
  }

  const position = getTooltipPosition()

  return createPortal(
    <>
      {/* Overlay */}
      <div className="fixed inset-0 z-50 bg-black/50" onClick={handleSkip} />

      {/* Highlight */}
      <div
        className="fixed z-50 rounded-lg ring-4 ring-primary ring-offset-4 transition-all duration-300"
        style={{
          left: targetRect.left - 4,
          top: targetRect.top - 4,
          width: targetRect.width + 8,
          height: targetRect.height + 8,
          boxShadow: '0 0 0 9999px rgba(0, 0, 0, 0.5)'
        }}
      />

      {/* Tooltip */}
      <Card
        className={cn(
          'fixed z-50 w-80 p-4 shadow-xl animate-in fade-in zoom-in-95 duration-200',
          placement === 'top' && 'slide-in-from-bottom-4',
          placement === 'bottom' && 'slide-in-from-top-4',
          placement === 'left' && 'slide-in-from-right-4',
          placement === 'right' && 'slide-in-from-left-4'
        )}
        style={{
          left: position.left,
          top: position.top,
          transform: position.transform
        }}
      >
        <div className="flex items-start justify-between mb-2">
          <h4 className="font-semibold">{step.title}</h4>
          <Button variant="ghost" size="icon" className="h-6 w-6" onClick={handleSkip}>
            <X className="h-4 w-4" />
          </Button>
        </div>
        <p className="text-sm text-muted-foreground mb-4">{step.content}</p>

        <div className="flex items-center justify-between">
          <div className="flex gap-1">
            {steps.map((_, i) => (
              <div
                key={i}
                className={cn(
                  'h-1.5 w-1.5 rounded-full transition-colors',
                  i === currentStep ? 'bg-primary' : 'bg-muted'
                )}
              />
            ))}
          </div>
          <div className="flex gap-2">
            {currentStep > 0 && (
              <Button variant="ghost" size="sm" onClick={handlePrev}>
                <ChevronLeft className="h-4 w-4 mr-1" />
                Back
              </Button>
            )}
            <Button size="sm" onClick={handleNext}>
              {currentStep === steps.length - 1 ? 'Finish' : 'Next'}
              {currentStep < steps.length - 1 && <ChevronRight className="h-4 w-4 ml-1" />}
            </Button>
          </div>
        </div>

        <div className="mt-2 text-center text-xs text-muted-foreground">
          Step {currentStep + 1} of {steps.length}
        </div>
      </Card>
    </>,
    document.body
  )
}

// Hook for managing tour state
export function useTour(tourKey: string) {
  const [hasCompletedTour, setHasCompletedTour] = useState(() => {
    return localStorage.getItem(`tour-${tourKey}`) === 'completed'
  })

  const [isOpen, setIsOpen] = useState(false)

  const startTour = () => setIsOpen(true)

  const completeTour = () => {
    setHasCompletedTour(true)
    localStorage.setItem(`tour-${tourKey}`, 'completed')
  }

  const resetTour = () => {
    setHasCompletedTour(false)
    localStorage.removeItem(`tour-${tourKey}`)
  }

  return {
    isOpen,
    setIsOpen,
    hasCompletedTour,
    startTour,
    completeTour,
    resetTour
  }
}

// Predefined tours
export const dashboardTour: TourStep[] = [
  {
    target: '[data-tour="sidebar"]',
    title: 'Navigation',
    content: 'Use the sidebar to navigate between different sections of the admin panel.',
    placement: 'right'
  },
  {
    target: '[data-tour="stats"]',
    title: 'Statistics',
    content: 'View real-time statistics about your load balancer configuration.',
    placement: 'bottom'
  },
  {
    target: '[data-tour="charts"]',
    title: 'Charts',
    content: 'Visualize traffic patterns and performance metrics.',
    placement: 'top'
  }
]

export const backendsTour: TourStep[] = [
  {
    target: '[data-tour="add-backend"]',
    title: 'Add Backend',
    content: 'Click here to add a new backend server to your pool.',
    placement: 'bottom'
  },
  {
    target: '[data-tour="backend-list"]',
    title: 'Backend List',
    content: 'View and manage all your backend servers here.',
    placement: 'top'
  }
]
