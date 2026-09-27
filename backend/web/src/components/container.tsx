import { cx } from "@/lib/cx";

export function Container({
  children,
  className,
  size = "lg",
}: {
  children: React.ReactNode;
  className?: string;
  size?: "sm" | "md" | "lg" | "xl";
}) {
  const max = {
    sm: "max-w-2xl",
    md: "max-w-4xl",
    lg: "max-w-6xl",
    xl: "max-w-7xl",
  }[size];
  return (
    <div className={cx("mx-auto w-full px-6 md:px-10", max, className)}>
      {children}
    </div>
  );
}
