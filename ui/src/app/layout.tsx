import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "glean-lite — codebase search",
  description: "RAG-powered search across GitHub repos",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
