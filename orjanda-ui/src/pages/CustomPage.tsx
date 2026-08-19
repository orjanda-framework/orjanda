import type { ComponentType } from 'react';
import { useParams } from 'react-router-dom';
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty';
import { FileQuestionIcon } from 'lucide-react';

const custom = new Map<string, ComponentType>();

export function registerCustomComponent(name: string, comp: ComponentType): void {
  custom.set(name, comp);
}

export function CustomPage() {
  const params = useParams();
  const componentName = (params as Record<string, string>).component ?? '';
  const Comp = custom.get(componentName);

  if (!Comp) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileQuestionIcon />
          </EmptyMedia>
          <EmptyTitle>Component not registered</EmptyTitle>
          <EmptyDescription>
            Custom component <code className="font-mono text-foreground">{componentName}</code> is
            not registered. Register it via <code className="font-mono text-foreground">registerCustomComponent</code>.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  return <Comp />;
}
