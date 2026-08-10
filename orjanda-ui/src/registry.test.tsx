// ComponentRegistry resolution order acceptance test (PRD §18.2):
// Document-specific → Application override → Default, all three registered
// simultaneously for the same field must resolve in that order.

import { describe, expect, it } from 'vitest';
import { ComponentRegistry, type NamedComponent } from './registry';

function render(key: string): string {
  return `rendered:${key}`;
}

function call(comp: NamedComponent | undefined): string {
  return (comp as unknown as () => string)();
}

describe('ComponentRegistry', () => {
  it('resolves with Document-specific before Application override before Default', () => {
    // All three levels registered simultaneously for the same field
    // (Employee.JoinDate is a Date field).
    ComponentRegistry.register('field:Date', () => render('default'));
    ComponentRegistry.register('field:Date:Employee', () => render('app-override'));
    ComponentRegistry.register('field:Date:Employee.JoinDate', () => render('doc-specific'));

    const resolved = ComponentRegistry.resolveField('Date', 'Employee', 'JoinDate');
    expect(resolved).toBeDefined();
    expect(call(resolved)).toBe('rendered:doc-specific');
  });

  it('falls back to the Application override when no Document-specific entry exists', () => {
    const resolved = ComponentRegistry.resolveField('Date', 'Employee', 'HireDate');
    expect(resolved).toBeDefined();
    expect(call(resolved)).toBe('rendered:app-override');
  });

  it('falls back to the Default renderer when neither override exists', () => {
    const resolved = ComponentRegistry.resolveField('Date', 'Contractor', 'StartDate');
    expect(resolved).toBeDefined();
    expect(call(resolved)).toBe('rendered:default');
  });

  it('returns undefined for an unregistered field type', () => {
    expect(ComponentRegistry.resolveField('widget', 'Thing', 'Field')).toBeUndefined();
  });

  it('re-registering a key replaces the previous component (Application override)', () => {
    ComponentRegistry.register('field:Date', () => render('default-v2'));
    const resolved = ComponentRegistry.resolveField('Date', 'Contractor', 'StartDate');
    expect(call(resolved)).toBe('rendered:default-v2');
  });
});
