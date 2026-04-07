import type { ComponentType } from "react";
import { cn } from "@/lib/utils";
import { feedItemStyles, sidebarItemIconStyles } from "./styles";

interface SidebarShortcutItemProps {
  label: string;
  icon: ComponentType<{ className?: string }>;
  isActive?: boolean;
  onClick?: () => void;
}

export function SidebarShortcutItem({
  label,
  icon: Icon,
  isActive = false,
  onClick,
}: SidebarShortcutItemProps) {
  return (
    <div
      data-active={isActive}
      className={cn(feedItemStyles, "pl-2.5")}
      onClick={onClick}
    >
      <span className={sidebarItemIconStyles}>
        <Icon className="size-4 text-primary" />
      </span>
      <span className="grow">{label}</span>
    </div>
  );
}
