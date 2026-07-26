import { Bot, Context, session, InlineKeyboard, webhookCallback } from 'grammy/web';
import {
  type Conversation,
  type ConversationFlavor,
  conversations,
  createConversation,
} from '@grammyjs/conversations';
import { KvAdapter } from '@grammyjs/storage-cloudflare';
import { CloudflareManager } from './cloudflare';

// @ts-ignore
type MyContext = Context & ConversationFlavor;
type MyConversation = Conversation<MyContext>;

let bot: Bot<MyContext>;

function initBot(env: any) {
  if (bot) return bot;
  bot = new Bot<MyContext>(env.BOT_TOKEN);
  
  bot.use(session({
    initial: () => ({}),
    storage: new KvAdapter(env.SESSION_KV)
  }));
  bot.use(conversations());

async function deployConversation(conversation: MyConversation, ctx: MyContext) {
  const permissions = [
    { key: "workers_scripts", type: "edit" },
    { key: "workers_kv_storage", type: "edit" },
    { key: "page", type: "edit" },
    { key: "dns", type: "edit" },
    { key: "user_details", type: "read" }
  ];
  
  const tokenUrl = new URL("https://dash.cloudflare.com/profile/api-tokens");
  tokenUrl.searchParams.set("permissionGroupKeys", JSON.stringify(permissions));
  tokenUrl.searchParams.set("accountId", "*");
  tokenUrl.searchParams.set("zoneId", "all");
  tokenUrl.searchParams.set("name", "BPB-Bot-Token");

  await ctx.reply(`🚀 <b>Welcome to the BPB Panel Deployer!</b>\n\nTo make this easy, click the link below to generate a pre-configured Cloudflare token. \n\n🔗 <a href="${tokenUrl.toString()}">Click here to generate Token</a>\n\n<i>(Just scroll down, click "Continue to summary", then "Create Token".)</i>\n\n<b>Please paste your new API Token below:</b>`, { parse_mode: "HTML", disable_web_page_preview: true });
  const tokenCtx = await conversation.wait();
  const token = (tokenCtx.message?.text || '').trim();

  if (!token) {
    await ctx.reply("❌ Invalid token format. Please run /deploy again.");
    return;
  }

  const msg = await ctx.reply("⏳ Validating Cloudflare Token...");
  const cf = new CloudflareManager(token);
  
  const accountInfo = await conversation.external(async () => {
    const isValid = await cf.verifyAndInitialize();
    return { isValid, accountId: cf.accountId, email: cf.email };
  });

  if (!accountInfo.isValid) {
    await ctx.api.editMessageText(ctx.chat!.id, msg.message_id, "❌ Invalid Cloudflare API Token. Please verify its permissions and run /deploy again.");
    return;
  }

  cf.accountId = accountInfo.accountId;
  cf.email = accountInfo.email;

  await ctx.api.editMessageText(ctx.chat!.id, msg.message_id, `✅ Token Validated!\nLogged in as: ${cf.email}`);

  const keyboard = new InlineKeyboard()
    .text("Workers", "deploy_workers")
    .text("Pages", "deploy_pages");

  await ctx.reply("Where would you like to deploy?", { reply_markup: keyboard });
  
  const typeCtx = await conversation.waitForCallbackQuery(["deploy_workers", "deploy_pages"]);
  await typeCtx.answerCallbackQuery();
  const deployType = typeCtx.callbackQuery.data === "deploy_workers" ? "workers" : "pages";

  await ctx.reply(`Selected: <b>${deployType.toUpperCase()}</b>\n\nPlease enter a subdomain name for your panel (e.g. 'my-panel'):`, { parse_mode: 'HTML' });
  const subdomainCtx = await conversation.wait();
  const workerName = subdomainCtx.message?.text?.trim().toLowerCase();

  if (!workerName || !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(workerName)) {
    await ctx.reply("❌ Invalid subdomain format. Use only lowercase letters, numbers, and dashes. Run /deploy again.");
    return;
  }

  const isTaken = await conversation.external(() => cf.isNameTaken(deployType, workerName));
  if (isTaken) {
    const overwriteKeyboard = new InlineKeyboard()
      .text("Yes, Overwrite", "overwrite_yes")
      .text("No, Cancel", "overwrite_no");
    await ctx.reply("⚠️ This subdomain is already taken on your account. Do you want to overwrite it? (This will reset the panel password and path)", { reply_markup: overwriteKeyboard });
    const overwriteCtx = await conversation.waitForCallbackQuery(["overwrite_yes", "overwrite_no"]);
    await overwriteCtx.answerCallbackQuery();
    if (overwriteCtx.callbackQuery.data === "overwrite_no") {
      await ctx.reply("Cancelled.");
      return;
    }
  }

  const progressMsg = await ctx.reply("⏳ 1/4 Creating KV Namespace...");
  
  try {
    const namespaceId = await conversation.external(() => cf.createKVNamespace(workerName, deployType));
    await ctx.api.editMessageText(ctx.chat!.id, progressMsg.message_id, "⏳ 2/4 Building Script (Fetching latest worker.js)...");

    let cfSubdomain = deployType === 'pages' ? 'pages.dev' : await conversation.external(() => cf.getWorkersDevSubdomain());
    if (deployType === 'workers' && !cfSubdomain) {
      // Need to create a random workers.dev subdomain if account doesn't have one
      const randomSub = 'bpb-' + Math.random().toString(36).substring(2, 8);
      cfSubdomain = await conversation.external(() => cf.createWorkersDevSubdomain(randomSub));
    }

    const { script, securePath } = await conversation.external(() => cf.buildScript(workerName, cfSubdomain!));

    await ctx.api.editMessageText(ctx.chat!.id, progressMsg.message_id, `⏳ 3/4 Deploying to ${deployType}...`);

    let finalUrl = '';
    if (deployType === 'workers') {
      await conversation.external(() => cf.deployWorker(workerName, script, namespaceId));
      await conversation.external(() => cf.enableWorkerSubdomain(workerName));
      finalUrl = `https://${workerName}.${cfSubdomain}/${securePath}/panel`;
    } else {
      const pageSubdomain = await conversation.external(() => cf.createPagesProject(workerName, namespaceId));
      await conversation.external(() => cf.deployPagesScript(workerName, script));
      finalUrl = `https://${pageSubdomain}/${securePath}/panel`;
    }

    await ctx.api.editMessageText(ctx.chat!.id, progressMsg.message_id, `✅ <b>Deployment Successful!</b>\n\n🔗 <b>Panel URL:</b> \n${finalUrl}\n\n⚠️ Keep this link secure.`, { parse_mode: 'HTML' });

    // Ask to set up Telegram Bot
    const tgKeyboard = new InlineKeyboard()
      .text("Yes, connect Bot", "setup_tg")
      .text("No, skip", "skip_tg");

    await ctx.reply("Would you like to connect a Telegram Bot to manage your new panel? This lets you manage subscriptions and check usage right from Telegram.", { reply_markup: tgKeyboard });

    const tgCtx = await conversation.waitForCallbackQuery(["setup_tg", "skip_tg"]);
    await tgCtx.answerCallbackQuery();

    if (tgCtx.callbackQuery.data === "setup_tg") {
      await ctx.reply("Please go to @BotFather, use /newbot to create a new bot, and paste the <b>HTTP API Token</b> here:", { parse_mode: 'HTML' });
      let botToken = "";
      let tokenMsg;
      while (true) {
        tokenMsg = await conversation.waitFor("message:text");
        botToken = tokenMsg.message.text.trim();
        
        if (botToken.startsWith(ctx.me.id.toString() + ':')) {
          await ctx.reply("❌ **You cannot use this bot's token!** This is the deployer bot.\n\nPlease go to @BotFather, create a **completely new bot**, and paste that new token here:");
          continue;
        }

        if (botToken && botToken.includes(":")) break;
        await ctx.reply("❌ Invalid token format. Please paste a valid HTTP API Token:");
      }

      if (!botToken || !botToken.includes(':')) {
        await ctx.reply("❌ Invalid token format. You will have to set up the bot manually from the panel.");
      } else {
        const setupMsg = await ctx.reply("⏳ Configuring your Telegram Bot...");
        try {
          await conversation.external(() => cf.setTelegramBotKV(namespaceId, botToken, String(ctx.from!.id)));
          
          const webhookUrl = finalUrl.replace('/panel', '/telegram/webhook');
          const webhookRes = await conversation.external(async () => {
            const res = await fetch(`https://api.telegram.org/bot${botToken}/setWebhook?url=${encodeURIComponent(webhookUrl)}`);
            return { ok: res.ok };
          });

          if (!webhookRes.ok) throw new Error("Failed to set webhook at Telegram API");

          await conversation.external(async () => {
            const res = await fetch(`https://api.telegram.org/bot${botToken}/setMyCommands`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                commands: [
                  { command: "start", description: "🤖 Show welcome menu" },
                  { command: "info", description: "ℹ️ Show panel information" },
                  { command: "get_client", description: "📥 Download V2ray clients" },
                  { command: "get_sub", description: "🔗 Get subscription link" },
                  { command: "my_usage", description: "📊 Check my usage" }
                ]
              })
            });
            return { ok: res.ok };
          });

          await ctx.api.editMessageText(ctx.chat!.id, setupMsg.message_id, "✅ <b>Telegram Bot successfully configured!</b>\n\nYou can now go to your new bot and type <code>/start</code>.", { parse_mode: 'HTML' });
        } catch (e: any) {
          console.error("TG Setup Error:", e);
          await ctx.api.editMessageText(ctx.chat!.id, setupMsg.message_id, `❌ Failed to configure Telegram Bot: ${e.message}\n\nYou can still set it up manually from the panel.`);
        }
      }
    }
  } catch (error: any) {
    console.error(error);
    await ctx.api.editMessageText(ctx.chat!.id, progressMsg.message_id, `❌ Deployment Failed:\n${error.message}`);
  }
}



async function manageConversation(conversation: MyConversation, ctx: MyContext) {
  await ctx.reply(`<b>Please paste your Cloudflare API Token to view deployments:</b>`, { parse_mode: "HTML" });
  const tokenCtx = await conversation.wait();
  const token = (tokenCtx.message?.text || '').trim();

  if (!token) {
    await ctx.reply("❌ Invalid token format. Please run /manage again.");
    return;
  }

  const msg = await ctx.reply("⏳ Validating Cloudflare Token...");
  const cf = new CloudflareManager(token);
  
  const accountInfo = await conversation.external(async () => {
    const isValid = await cf.verifyAndInitialize();
    return { isValid, accountId: cf.accountId, email: cf.email };
  });

  if (!accountInfo.isValid) {
    await ctx.api.editMessageText(ctx.chat!.id, msg.message_id, "❌ Invalid Cloudflare API Token. Please verify its permissions and run /manage again.");
    return;
  }

  cf.accountId = accountInfo.accountId;
  cf.email = accountInfo.email;

  await ctx.api.editMessageText(ctx.chat!.id, msg.message_id, `✅ Token Validated!\nLogged in as: ${cf.email}`);

  const keyboard = new InlineKeyboard()
    .text("Workers", "manage_workers")
    .text("Pages", "manage_pages");

  await ctx.reply("Which deployments would you like to manage?", { reply_markup: keyboard });
  
  const typeCtx = await conversation.waitForCallbackQuery(["manage_workers", "manage_pages"]);
  await typeCtx.answerCallbackQuery();
  const deployType = typeCtx.callbackQuery.data === "manage_workers" ? "workers" : "pages";

  const loadMsg = await ctx.reply(`⏳ Loading ${deployType}...`);
  const deployments = await conversation.external(() => cf.listDeployments(deployType));

  if (deployments.length === 0) {
    await ctx.api.editMessageText(ctx.chat!.id, loadMsg.message_id, `No ${deployType} deployments found.`);
    return;
  }

  const listKeyboard = new InlineKeyboard();
  deployments.slice(0, 10).forEach(d => {
    listKeyboard.text(d.name || d.id, `select_${d.id || d.name}`).row();
  });
  listKeyboard.text("Cancel", "cancel_manage");

  await ctx.api.editMessageText(ctx.chat!.id, loadMsg.message_id, `Select a deployment to manage:`, { reply_markup: listKeyboard });

  const selectTriggers = deployments.slice(0, 10).map(d => `select_${d.id || d.name}`);
  selectTriggers.push("cancel_manage");
  const selectCtx = await conversation.waitForCallbackQuery(selectTriggers);
  await selectCtx.answerCallbackQuery();
  
  if (selectCtx.callbackQuery.data === "cancel_manage") {
    await ctx.reply("Cancelled.");
    return;
  }

  const selectedId = selectCtx.callbackQuery.data.replace("select_", "");
  const selectedDep = deployments.find(d => (d.id || d.name) === selectedId);
  const selectedName = selectedDep?.name || selectedId;

  const actionKeyboard = new InlineKeyboard()
    .text("🗑 Delete Deployment", `delete_${selectedName}`).row()
    .text("Cancel", "cancel_action");

  await ctx.reply(`Selected: <b>${selectedName}</b>\nWhat would you like to do?`, { parse_mode: 'HTML', reply_markup: actionKeyboard });

  const actionTriggers = [`delete_${selectedName}`, "cancel_action"];
  const actionCtx = await conversation.waitForCallbackQuery(actionTriggers);
  await actionCtx.answerCallbackQuery();

  if (actionCtx.callbackQuery.data === "cancel_action") {
    await ctx.reply("Cancelled.");
    return;
  }

  if (actionCtx.callbackQuery.data.startsWith("delete_")) {
    const delMsg = await ctx.reply(`⏳ Deleting ${selectedName}...`);
    try {
      await conversation.external(() => cf.deleteDeployment(deployType, selectedName));
      await ctx.api.editMessageText(ctx.chat!.id, delMsg.message_id, `✅ Successfully deleted ${selectedName}.`);
    } catch (e: any) {
      await ctx.api.editMessageText(ctx.chat!.id, delMsg.message_id, `❌ Failed to delete ${selectedName}: ${e.message}`);
    }
  }
}

  bot.use(createConversation(deployConversation));
  bot.use(createConversation(manageConversation));

  bot.command('start', (ctx) => ctx.reply("Welcome to BPB Wizard Telegram Bot! Type /deploy to start deploying, or /manage to edit/delete deployments."));
  bot.command('deploy', (ctx) => ctx.conversation.enter('deployConversation'));
  bot.command('manage', (ctx) => ctx.conversation.enter('manageConversation'));

  return bot;
}

export default {
  async fetch(request: any, env: any, ctx: any) {
    try {
      const bot = initBot(env);
      return await webhookCallback(bot, 'cloudflare-mod', { timeoutMilliseconds: 60000 })(request);
    } catch (e: any) {
      return new Response(e.stack || e.message, { status: 500 });
    }
  }
};
