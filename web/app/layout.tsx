import type { Metadata } from "next";

import "./globals.css";

export const metadata: Metadata = {
  description: "Geração de relatórios de dados brasileiros sobre queimaduras.",
  title: "Enceladus Big Data",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="pt-BR">
      <body>{children}</body>
    </html>
  );
}
