import { createBrowserRouter, Navigate, RouterProvider } from 'react-router-dom';
import { TooltipProvider } from '@/components/ui/tooltip';
import { AuthProvider, useAuth } from '@/core/AuthProvider';
import { MetaProvider } from '@/core/MetaProvider';
import { AgentChatPage } from '@/pages/AgentChatPage';
import { CustomPage } from '@/pages/CustomPage';
import { DashboardPage } from '@/pages/DashboardPage';
import { DocFormPage } from '@/pages/DocFormPage';
import { DocListPage } from '@/pages/DocListPage';
import { LoginPage } from '@/pages/LoginPage';
import { Shell } from '@/pages/Shell';

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth();
  if (!isAuthenticated) return <Navigate to="/login" replace />;
  return children;
}

function RequireGuest({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth();
  if (isAuthenticated) return <Navigate to="/" replace />;
  return children;
}

const router = createBrowserRouter([
  { path: '/login', element: <RequireGuest><LoginPage /></RequireGuest> },
  {
    path: '/',
    element: (
      <RequireAuth>
        <Shell />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'agent', element: <AgentChatPage /> },
      { path: 'doc/:doctype', element: <DocListPage /> },
      { path: 'doc/:doctype/new', element: <DocFormPage /> },
      { path: 'doc/:doctype/:id', element: <DocFormPage /> },
      { path: 'page/:component', element: <CustomPage /> },
    ],
  },
]);

export default function App() {
  return (
    <AuthProvider>
      <MetaProvider>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </MetaProvider>
    </AuthProvider>
  );
}
