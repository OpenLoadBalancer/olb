import { useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { AlertCircle, Check } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface ValidationRule {
  required?: boolean
  minLength?: number
  maxLength?: number
  pattern?: RegExp
  email?: boolean
  url?: boolean
  min?: number
  max?: number
  custom?: (value: any) => boolean
  message?: string
}

export interface FieldConfig {
  [key: string]: ValidationRule
}

export interface FieldErrors {
  [key: string]: string | null
}

export interface FormValues {
  [key: string]: any
}

export function useForm<T extends FormValues>(
  initialValues: T,
  validationRules: FieldConfig = {}
) {
  const [values, setValues] = useState<T>(initialValues)
  const [errors, setErrors] = useState<FieldErrors>({})
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const [isSubmitting, setIsSubmitting] = useState(false)

  const validateField = useCallback(
    (name: string, value: any): string | null => {
      const rules = validationRules[name]
      if (!rules) return null

      if (rules.required && (!value || value === '')) {
        return rules.message || 'This field is required'
      }

      if (value) {
        if (rules.minLength && String(value).length < rules.minLength) {
          return rules.message || `Minimum ${rules.minLength} characters required`
        }

        if (rules.maxLength && String(value).length > rules.maxLength) {
          return rules.message || `Maximum ${rules.maxLength} characters allowed`
        }

        if (rules.pattern && !rules.pattern.test(String(value))) {
          return rules.message || 'Invalid format'
        }

        if (rules.email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(value))) {
          return rules.message || 'Invalid email address'
        }

        if (rules.url && !/^(https?:\/\/)?([\da-z.-]+)\.([a-z.]{2,6})([/\w .-]*)*\/?$/.test(String(value))) {
          return rules.message || 'Invalid URL'
        }

        if (rules.min !== undefined && Number(value) < rules.min) {
          return rules.message || `Minimum value is ${rules.min}`
        }

        if (rules.max !== undefined && Number(value) > rules.max) {
          return rules.message || `Maximum value is ${rules.max}`
        }

        if (rules.custom && !rules.custom(value)) {
          return rules.message || 'Invalid value'
        }
      }

      return null
    },
    [validationRules]
  )

  const validateAll = useCallback((): boolean => {
    const newErrors: FieldErrors = {}
    let isValid = true

    Object.keys(validationRules).forEach((field) => {
      const error = validateField(field, values[field])
      if (error) {
        newErrors[field] = error
        isValid = false
      }
    })

    setErrors(newErrors)
    return isValid
  }, [validateField, validationRules, values])

  const handleChange = useCallback(
    (name: string, value: any) => {
      setValues((prev) => ({ ...prev, [name]: value }))
      if (touched[name]) {
        setErrors((prev) => ({ ...prev, [name]: validateField(name, value) }))
      }
    },
    [touched, validateField]
  )

  const handleBlur = useCallback(
    (name: string) => {
      setTouched((prev) => ({ ...prev, [name]: true }))
      setErrors((prev) => ({ ...prev, [name]: validateField(name, values[name]) }))
    },
    [validateField, values]
  )

  const handleSubmit = useCallback(
    async (onSubmit: (values: T) => Promise<void> | void) => {
      setIsSubmitting(true)
      if (validateAll()) {
        try {
          await onSubmit(values)
        } catch (error) {
          console.error('Form submission error:', error)
        }
      }
      setIsSubmitting(false)
    },
    [validateAll, values]
  )

  const reset = useCallback(() => {
    setValues(initialValues)
    setErrors({})
    setTouched({})
    setIsSubmitting(false)
  }, [initialValues])

  const setValue = useCallback((name: string, value: any) => {
    setValues((prev) => ({ ...prev, [name]: value }))
  }, [])

  return {
    values,
    errors,
    touched,
    isSubmitting,
    handleChange,
    handleBlur,
    handleSubmit,
    reset,
    setValue,
    validateAll,
    setValues
  }
}

interface FormFieldProps {
  label: string
  name: string
  error?: string | null
  touched?: boolean
  required?: boolean
  children: React.ReactNode
  className?: string
  helpText?: string
}

export function FormField({
  label,
  name,
  error,
  touched,
  required,
  children,
  className,
  helpText
}: FormFieldProps) {
  const showError = touched && error

  return (
    <div className={cn('space-y-2', className)}>
      <Label htmlFor={name}>
        {label}
        {required && <span className="text-destructive ml-1">*</span>}
      </Label>
      {children}
      {showError ? (
        <p className="text-sm text-destructive flex items-center gap-1">
          <AlertCircle className="h-3 w-3" />
          {error}
        </p>
      ) : helpText ? (
        <p className="text-sm text-muted-foreground">{helpText}</p>
      ) : null}
    </div>
  )
}

interface FormErrorProps {
  error: string | null
}

export function FormError({ error }: FormErrorProps) {
  if (!error) return null

  return (
    <Alert variant="destructive" className="mb-4">
      <AlertCircle className="h-4 w-4" />
      <AlertDescription>{error}</AlertDescription>
    </Alert>
  )
}

interface FormSuccessProps {
  message: string | null
}

export function FormSuccess({ message }: FormSuccessProps) {
  if (!message) return null

  return (
    <Alert className="mb-4 border-green-500/50 text-green-600 dark:text-green-400">
      <Check className="h-4 w-4" />
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  )
}
