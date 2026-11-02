import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "SQL View Builder",
  description: "เลือกตารางและคอลัมน์เพื่อสร้าง SQL view",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="th">
      <body>{children}</body>
    </html>
  );
}
