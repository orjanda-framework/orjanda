import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuth } from '@/core/AuthProvider';
import { useMeta } from '@/core/MetaProvider';
import {
  SidebarProvider,
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarTrigger,
  SidebarSeparator,
  SidebarInset,
} from '@/components/ui/sidebar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import {
  LayoutDashboardIcon,
  MessageSquareIcon,
  LogOutIcon,
  ChevronsUpDownIcon,
} from 'lucide-react';

export function Shell() {
  const { identity, logout } = useAuth();
  const { summaries, pages, loading } = useMeta();
  const navigate = useNavigate();

  const groups = new Map<string, typeof summaries>();
  for (const s of summaries) {
    const key = s.module ?? 'Other';
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(s);
  }

  function signOut() {
    logout();
    navigate('/login');
  }

  const initials = identity?.name
    ? identity.name.split(' ').map((n: string) => n[0]).join('').toUpperCase().slice(0, 2)
    : identity?.email?.slice(0, 2).toUpperCase() ?? 'U';

  return (
    <SidebarProvider>
      <Sidebar>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupLabel>
              <span className="font-semibold">Orjanda</span>
            </SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton render={<NavLink to="/" />}>
                    <LayoutDashboardIcon />
                    <span>Dashboard</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                <SidebarMenuItem>
                  <SidebarMenuButton render={<NavLink to="/agent" />}>
                    <MessageSquareIcon />
                    <span>Agent Chat</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarSeparator />

          {loading ? (
            <SidebarGroup>
              <SidebarGroupContent>
                <SidebarMenu>
                  {Array.from({ length: 3 }).map((_, i) => (
                    <SidebarMenuItem key={i}>
                      <div className="flex h-8 items-center gap-2 rounded-md px-2">
                        <Skeleton className="size-4 rounded-md" />
                        <Skeleton className="h-4 flex-1" />
                      </div>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ) : (
            [...groups.entries()].map(([group, docs]) => (
              <SidebarGroup key={group}>
                <SidebarGroupLabel>{group}</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {docs.map((d) => (
                      <SidebarMenuItem key={d.name}>
                        <SidebarMenuButton
                          render={<NavLink to={`/doc/${d.name}`} />}
                        >
                          <span>{d.name}</span>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    ))}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            ))
          )}

          {!loading && pages.length > 0 && (
            <>
              <SidebarSeparator />
              <SidebarGroup>
                <SidebarGroupLabel>Custom</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {pages.map((p) => (
                      <SidebarMenuItem key={p.path}>
                        <SidebarMenuButton
                          render={<NavLink to={p.path} />}
                        >
                          <span>{p.title}</span>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    ))}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            </>
          )}
        </SidebarContent>

        <SidebarSeparator />

        <SidebarFooter>
          <SidebarMenu>
            <SidebarMenuItem>
              <DropdownMenu>
                <DropdownMenuTrigger render={<SidebarMenuButton />}>
                  <Avatar className="size-6">
                    <AvatarFallback className="text-xs">{initials}</AvatarFallback>
                  </Avatar>
                  <span className="truncate text-sm">
                    {identity?.name ?? identity?.email ?? 'User'}
                  </span>
                  <ChevronsUpDownIcon className="ms-auto text-muted-foreground" />
                </DropdownMenuTrigger>
                <DropdownMenuContent side="top" className="w-[--radix-dropdown-menu-trigger-width]">
                  <DropdownMenuItem onClick={signOut}>
                    <LogOutIcon />
                    <span>Sign out</span>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
      </Sidebar>

      <SidebarInset>
        <header className="flex h-12 items-center gap-2 border-b px-4">
          <SidebarTrigger />
          <Separator orientation="vertical" className="h-4" />
        </header>
        <main className="flex-1 p-6">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
