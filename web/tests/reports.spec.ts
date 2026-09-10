import { expect, test } from "@playwright/test";

const reportTypes = [
  { id: "DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL", nome: "Densidade geral", multiplos_estados: true, path: "/relatorios/queimaduras/densidade-municipal-por-periodo-geral" },
  { id: "DENSIDADE_MUNICIPAL_POR_PERIODO", nome: "Densidade municipal", multiplos_estados: false, path: "/relatorios/queimaduras/densidade-municipal-por-periodo" },
  { id: "CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO", nome: "Casos mensais", multiplos_estados: true, path: "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado" },
];

test.beforeEach(async ({ page }) => {
  await page.route("http://localhost:8000/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    const responses: Record<string, unknown> = {
      "/config/anos": Array.from({ length: 11 }, (_, index) => 2014 + index),
      "/config/estados": [{ DF: "Distrito Federal" }, { MG: "Minas Gerais" }],
      "/config/relatorios": reportTypes,
      "/relatorios/processados": [],
    };
    if (path in responses) {
      await route.fulfill({ json: responses[path] });
    } else if (route.request().method() === "POST") {
      await route.fulfill({ status: 202, json: { id_requisicao: "test-request", destino: "test@example.invalid" } });
    } else {
      await route.fulfill({ status: 404 });
    }
  });
});

test("submits dates after 2019 and preserves multiple/single state selection", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("Nenhum relatório foi processado ainda.")).toBeVisible();
  await expect(page.getByLabel("Data inicial")).toHaveValue("2023-01-01");
  await expect(page.getByLabel("Data final")).toHaveValue("2024-12-31");
  const faviconUrl = await page.locator('link[rel="icon"]').getAttribute("href");
  expect(faviconUrl).toBeTruthy();
  expect((await page.request.get(new URL(faviconUrl!, page.url()).toString())).status()).toBe(200);
  await page.getByRole("checkbox", { name: "Distrito Federal" }).check();
  await page.getByRole("checkbox", { name: "Minas Gerais" }).check();
  await page.getByLabel("Data inicial").fill("2024-01-01");
  await page.getByLabel("Data final").fill("2024-12-31");
  await expect(page.getByLabel("Data final")).toHaveAttribute("max", "2024-12-31");
  await page.getByLabel("E-mail").fill("test@example.invalid");
  const submitted = page.waitForRequest((request) => request.method() === "POST");
  await page.getByRole("button", { name: "Processar relatório" }).click();
  const requestUrl = new URL((await submitted).url());
  expect(requestUrl.searchParams.getAll("estado")).toEqual(["DF", "MG"]);
  expect(requestUrl.searchParams.get("data_inicio")).toBe("2024-01-01");
  expect(requestUrl.searchParams.get("data_fim")).toBe("2024-12-31");
  await expect(page.getByRole("status")).toContainText("test-request");

  await page.getByLabel("Tipo de relatório").selectOption(reportTypes[1].id);
  await page.getByRole("radio", { name: "Distrito Federal" }).check();
  await page.getByRole("radio", { name: "Minas Gerais" }).check();
  await expect(page.getByRole("radio", { name: "Distrito Federal" })).not.toBeChecked();
  await expect(page.getByRole("radio", { name: "Minas Gerais" })).toBeChecked();

  await page.getByLabel("Tipo de relatório").selectOption(reportTypes[2].id);
  await page.getByRole("button", { name: "Selecionar todos" }).click();
  const monthly = page.waitForRequest((request) => request.method() === "POST");
  await page.getByRole("button", { name: "Processar relatório" }).click();
  const monthlyUrl = new URL((await monthly).url());
  expect(monthlyUrl.searchParams.get("ano_inicio")).toBe("2024");
  expect(monthlyUrl.searchParams.get("ano_fim")).toBe("2024");
  await page.getByRole("button", { name: "Limpar" }).click();
  await expect(page.getByRole("checkbox", { name: "Distrito Federal" })).not.toBeChecked();
});

test("renders processed reports and keeps the Latte palette on mobile", async ({ page }) => {
  await page.route("**/relatorios/processados", (route) => route.fulfill({ json: [{
    tipo: "Densidade geral", estado: "DF", data_inicio: "2024-01-01", data_fim: "2024-12-31",
    data_processamento: "10/09/2026 12:00:00", uri: "/relatorios/example.pdf",
  }] }));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("link", { name: "Baixar PDF" })).toHaveAttribute("href", "http://localhost:8000/relatorios/example.pdf");
  await expect(page.locator("body")).toHaveCSS("background-color", "rgb(239, 241, 245)");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("shows configuration and submission errors", async ({ page }) => {
  await page.route("**/config/anos", (route) => route.fulfill({ status: 500 }));
  await page.goto("/");
  await expect(page.getByRole("alert").filter({ hasText: "Não foi possível carregar a configuração" })).toBeVisible();
  await page.unroute("**/config/anos");
  await page.reload();
  await page.getByRole("checkbox", { name: "Distrito Federal" }).check();
  await page.getByLabel("Data inicial").fill("2024-01-01");
  await page.getByLabel("Data final").fill("2024-12-31");
  await page.getByLabel("E-mail").fill("test@example.invalid");
  await page.route("**/relatorios/queimaduras/**", (route) => route.fulfill({ status: 500 }));
  await page.getByRole("button", { name: "Processar relatório" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Não foi possível solicitar o relatório" })).toBeVisible();
});
