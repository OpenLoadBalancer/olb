import * as React from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { AlertTriangle, Loader2 } from 'lucide-react'

interface ConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  confirmText?: string
  cancelText?: string
  variant?: 'destructive' | 'default'
  onConfirm: () => void | Promise<void>
  isLoading?: boolean
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmText = 'Confirm',
  cancelText = 'Cancel',
  variant = 'default',
  onConfirm,
  isLoading
}: ConfirmDialogProps) {
  const handleConfirm = async () => {
    await onConfirm()
    if (!isLoading) {
      onOpenChange(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader className="gap-4">
          <div className="flex items-center gap-3">
            {variant === 'destructive' && (
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-destructive/10">
                <AlertTriangle className="h-5 w-5 text-destructive" />
              </div>
            )}
            <DialogTitle>{title}</DialogTitle>
          </div>
          <DialogDescription className="text-left">
            {description}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter className="gap-2 sm:gap-0">
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isLoading}
          >
            {cancelText}
          </Button>
          <Button
            variant={variant === 'destructive' ? 'destructive' : 'default'}
            onClick={handleConfirm}
            disabled={isLoading}
          >
            {isLoading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {confirmText}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface UseConfirmDialogOptions {
  title: string
  description: string
  confirmText?: string
  cancelText?: string
  variant?: 'destructive' | 'default'
}

export function useConfirmDialog() {
  const [open, setOpen] = React.useState(false)
  const [isLoading, setIsLoading] = React.useState(false)
  const [options, setOptions] = React.useState<UseConfirmDialogOptions>({
    title: '',
    description: ''
  })
  const resolveRef = React.useRef<((value: boolean) => void) | null>(null)
  const onConfirmRef = React.useRef<(() => void | Promise<void>) | null>(null)

  const confirm = React.useCallback(
    (opts: UseConfirmDialogOptions, onConfirm: () => void | Promise<void>): Promise<boolean> => {
      setOptions(opts)
      onConfirmRef.current = onConfirm
      setOpen(true)
      return new Promise((resolve) => {
        resolveRef.current = resolve
      })
    },
    []
  )

  const handleConfirm = async () => {
    if (onConfirmRef.current) {
      setIsLoading(true)
      try {
        await onConfirmRef.current()
        resolveRef.current?.(true)
      } catch (error) {
        resolveRef.current?.(false)
      } finally {
        setIsLoading(false)
        setOpen(false)
      }
    }
  }

  const handleCancel = () => {
    resolveRef.current?.(false)
    setOpen(false)
  }

  const dialog = (
    <ConfirmDialog
      open={open}
      onOpenChange={(open) => {
        if (!open) handleCancel()
        setOpen(open)
      }}
      title={options.title}
      description={options.description}
      confirmText={options.confirmText}
      cancelText={options.cancelText}
      variant={options.variant}
      onConfirm={handleConfirm}
      isLoading={isLoading}
    />
  )

  return { confirm, dialog }
}
