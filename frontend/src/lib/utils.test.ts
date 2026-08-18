import { describe, it, expect } from 'vitest'
import { cn } from './utils'

describe('cn', () => {
  it('fusionne des classes simples', () => {
    expect(cn('a', 'b')).toBe('a b')
  })

  it('ignore les valeurs falsy', () => {
    expect(cn('a', false && 'b', undefined, 'c')).toBe('a c')
  })
})
