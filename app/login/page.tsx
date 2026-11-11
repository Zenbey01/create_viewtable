"use client";

import { FormEvent, useState } from "react";

export default function LoginPage() {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    const password = new FormData(event.currentTarget).get("password");
    try {
      const response = await fetch("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password }),
      });
      if (response.ok) {
        window.location.href = "/";
        return;
      }
      const body = await response.json().catch(() => ({}));
      setError(body.error ?? "เข้าสู่ระบบไม่สำเร็จ");
    } catch {
      setError("เชื่อมต่อเซิร์ฟเวอร์ไม่สำเร็จ");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="loginWrap">
      <form className="loginCard" onSubmit={submit}>
        <h1>SQL View Builder</h1>
        <label htmlFor="password">รหัสผ่าน</label>
        <input id="password" name="password" type="password" autoComplete="current-password" required autoFocus />
        {error && (
          <p role="alert" className="loginError">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy}>
          {busy ? "กำลังตรวจสอบ..." : "เข้าสู่ระบบ"}
        </button>
      </form>
    </main>
  );
}
