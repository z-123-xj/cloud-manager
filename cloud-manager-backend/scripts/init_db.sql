-- 域名表
CREATE TABLE IF NOT EXISTS `domains` (
    `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `account_id` varchar(50) NOT NULL COMMENT '关联 cloud_accounts.account_id',
    `platform` varchar(50) NOT NULL COMMENT '云平台: aliyun, tencent, aws',
    `account_name` varchar(100) NOT NULL COMMENT '账号名称',
    `source` varchar(20) NOT NULL DEFAULT 'sync' COMMENT '来源: sync(云API同步)/manual(手动添加)',
    `domain_name` varchar(255) NOT NULL COMMENT '域名',
    `owner` varchar(100) DEFAULT NULL COMMENT '所有者',
    `auto_renew_enabled` tinyint(1) DEFAULT 0 COMMENT '自动续费: 0-关闭, 1-开启',
    `registration_date` datetime DEFAULT NULL COMMENT '注册日期',
    `expiration_date` datetime DEFAULT NULL COMMENT '到期日期',
    `expiration_days_left` int DEFAULT 0 COMMENT '剩余天数',
    `expiration_status_code` varchar(10) DEFAULT NULL COMMENT '到期状态码: 1-未到期, 2-已到期, 3-赎回期',
    `remark` varchar(500) DEFAULT NULL COMMENT '备注',
    `is_offline` tinyint(1) NOT NULL DEFAULT 0 COMMENT '是否下线: 0-在线, 1-下线',
    `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_account_domain` (`account_id`, `domain_name`),
    KEY `idx_platform` (`platform`),
    KEY `idx_account_name` (`account_name`),
    KEY `idx_expiration_date` (`expiration_date`),
    KEY `idx_expiration_days_left` (`expiration_days_left`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='域名信息表';


-- 创建用户表
CREATE TABLE IF NOT EXISTS `users` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT '用户ID',
    `username` VARCHAR(50) NOT NULL COMMENT '用户名',
    `password` VARCHAR(255) NOT NULL COMMENT '密码（加密存储）',
    `email` VARCHAR(100) NOT NULL COMMENT '邮箱',
    `real_name` VARCHAR(50) NOT NULL DEFAULT '' COMMENT '真实姓名',
    `role` VARCHAR(20) NOT NULL DEFAULT 'user' COMMENT '角色: admin/user/readonly',
    `login_attempts` INT NOT NULL DEFAULT 0 COMMENT '连续登录失败次数',
    `locked` TINYINT(1) NOT NULL DEFAULT 0 COMMENT '账号是否被锁定：0-否 1-是',
    `add_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_username` (`username`),
    UNIQUE INDEX `idx_email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表';

-- 阿里云账号管理表
CREATE TABLE IF NOT EXISTS `cloud_accounts` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `account_id` VARCHAR(50) NOT NULL DEFAULT '' COMMENT '账号业务ID（云厂商主账号ID，如 AWS 12位账号ID / 阿里云 UID）',
    `platform` VARCHAR(20) NOT NULL DEFAULT 'aliyun' COMMENT '平台：aliyun/aws/tencent',
    `site` VARCHAR(10) NOT NULL DEFAULT 'cn' COMMENT '站点：cn(国内站)/intl(国际站)',
    `name` VARCHAR(100) NOT NULL COMMENT '账号名称/备注',
    `access_key` VARCHAR(100) NOT NULL COMMENT 'AccessKey ID（明文存储）',
    `access_secret` VARCHAR(255) NOT NULL COMMENT 'AccessKey Secret（AES加密存储）',
    `enabled` TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否启用：1-启用 0-禁用',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE INDEX `uq_cloud_accounts_account_id` (`account_id`),
    UNIQUE INDEX `uq_cloud_accounts_access_key` (`access_key`),
    INDEX `idx_platform_site` (`platform`, `site`),
    INDEX `idx_platform` (`platform`),
    INDEX `idx_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='云平台账号管理表';

-- ECS实例表
CREATE TABLE IF NOT EXISTS `ecs_instances` (
    `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `account_id` varchar(50) NOT NULL COMMENT '云账号业务ID',
    `platform` varchar(20) NOT NULL DEFAULT 'aliyun' COMMENT '云平台: aliyun/tencent/aws',
    `site` varchar(10) NOT NULL DEFAULT 'cn' COMMENT '站点: cn=国内站, intl=国际站',
    `account_name` varchar(100) NOT NULL COMMENT '账号别名',
    `instance_id` varchar(50) NOT NULL COMMENT '实例ID (阿里云: i-bpxxx)',
    `instance_name` varchar(200) DEFAULT NULL COMMENT '实例名称',
    `description` varchar(500) DEFAULT NULL COMMENT '实例描述',
    `status` varchar(20) NOT NULL COMMENT '实例状态: Running/Stopped/Starting/Stopping',
    `cpu` bigint DEFAULT 0 COMMENT 'CPU核数',
    `memory` bigint DEFAULT 0 COMMENT '内存(GB)',
    `instance_type` varchar(50) DEFAULT NULL COMMENT '实例规格: ecs.g7.xlarge',
    `os_name` varchar(200) DEFAULT NULL COMMENT '操作系统名称',
    `public_ip_address` varchar(50) DEFAULT NULL COMMENT '公网IP (第一块网卡的公网IP)',
    `eip_address` varchar(50) DEFAULT NULL COMMENT '弹性公网IP (EIP绑定地址)',
    `private_ip_address` varchar(50) DEFAULT NULL COMMENT '私网IP (第一块网卡的私网IP)',
    `ssh_port` int NOT NULL DEFAULT 22 COMMENT 'SSH端口(堡垒机连接用)，云同步默认22，平台可手动维护',
    `region_name` varchar(50) DEFAULT NULL COMMENT '地域中文名: 香港',
    `zone_name` varchar(50) DEFAULT NULL COMMENT '可用区中文名: 香港 可用区B',
    `creation_time` datetime DEFAULT NULL COMMENT 'ECS实例创建时间 (阿里云API返回)',
    `is_offline` tinyint(1) NOT NULL DEFAULT 0 COMMENT '是否下线: 0=在线, 1=已下线',
    `source` varchar(20) NOT NULL DEFAULT 'sync' COMMENT '来源: sync(云API同步)/manual(手动添加)',
    `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '记录入库时间',
    `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '记录更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_account_instance` (`account_id`, `instance_id`),
    KEY `idx_platform` (`platform`),
    KEY `idx_status` (`status`),
    KEY `idx_region_name` (`region_name`),
    KEY `idx_is_offline` (`is_offline`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='ECS实例表';

-- 云平台地域管理表
CREATE TABLE IF NOT EXISTS `cloud_regions` (
    `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `platform` varchar(50) NOT NULL COMMENT '平台: aliyun/tencent/aws',
    `region_id` varchar(100) NOT NULL COMMENT '地域ID: ap-southeast-1',
    `region_name` varchar(100) NOT NULL COMMENT '地域中文名: 新加坡',
    `region_endpoint` varchar(255) DEFAULT '' COMMENT 'API endpoint',
    `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '是否启用: 0=禁用 1=启用',
    `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_platform_region` (`platform`, `region_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='云平台地域管理表';

-- 地域种子数据（幂等：已存在的 platform+region_id 组合自动跳过，只增不改；每次 init 校验补齐）
INSERT IGNORE INTO `cloud_regions` (`platform`, `region_id`, `region_name`, `region_endpoint`, `enabled`) VALUES
('aliyun', 'cn-hangzhou', '华东1(杭州)', 'ecs.cn-hangzhou.aliyuncs.com', 1),
('aliyun', 'cn-shanghai', '华东2(上海)', 'ecs.cn-shanghai.aliyuncs.com', 1),
('aliyun', 'cn-nanjing', '华东5(南京-本地地域)', 'ecs.cn-nanjing.aliyuncs.com', 1),
('aliyun', 'cn-fuzhou', '华东6(福州-本地地域)', 'ecs.cn-fuzhou.aliyuncs.com', 1),
('aliyun', 'cn-wuhan-lr', '华中1(武汉-本地地域)', 'ecs.cn-wuhan-lr.aliyuncs.com', 1),
('aliyun', 'cn-beijing', '华北2(北京)', 'ecs.cn-beijing.aliyuncs.com', 1),
('aliyun', 'cn-zhangjiakou', '华北3(张家口)', 'ecs.cn-zhangjiakou.aliyuncs.com', 1),
('aliyun', 'cn-huhehaote', '华北5(呼和浩特)', 'ecs.cn-huhehaote.aliyuncs.com', 1),
('aliyun', 'cn-wulanchabu', '华北6(乌兰察布)', 'ecs.cn-wulanchabu.aliyuncs.com', 1),
('aliyun', 'cn-shenzhen', '华南1(深圳)', 'ecs.cn-shenzhen.aliyuncs.com', 1),
('aliyun', 'cn-heyuan', '华南2(河源)', 'ecs.cn-heyuan.aliyuncs.com', 1),
('aliyun', 'cn-guangzhou', '华南3(广州)', 'ecs.cn-guangzhou.aliyuncs.com', 1),
('aliyun', 'cn-chengdu', '西南1(成都)', 'ecs.cn-chengdu.aliyuncs.com', 1),
('aliyun', 'cn-hongkong', '香港', 'ecs.cn-hongkong.aliyuncs.com', 1),
('aliyun', 'ap-southeast-1', '新加坡', 'ecs.ap-southeast-1.aliyuncs.com', 1),
('aliyun', 'ap-southeast-2', '澳大利亚(悉尼)', 'ecs.ap-southeast-2.aliyuncs.com', 1),
('aliyun', 'ap-southeast-3', '马来西亚(吉隆坡)', 'ecs.ap-southeast-3.aliyuncs.com', 1),
('aliyun', 'ap-southeast-5', '印度尼西亚(雅加达)', 'ecs.ap-southeast-5.aliyuncs.com', 1),
('aliyun', 'ap-southeast-6', '菲律宾(马尼拉)', 'ecs.ap-southeast-6.aliyuncs.com', 1),
('aliyun', 'ap-southeast-7', '泰国(曼谷)', 'ecs.ap-southeast-7.aliyuncs.com', 1),
('aliyun', 'ap-south-1', '印度(孟买)', 'ecs.ap-south-1.aliyuncs.com', 1),
('aliyun', 'ap-northeast-1', '日本(东京)', 'ecs.ap-northeast-1.aliyuncs.com', 1),
('aliyun', 'ap-northeast-2', '韩国(首尔)', 'ecs.ap-northeast-2.aliyuncs.com', 1),
('aliyun', 'eu-central-1', '德国(法兰克福)', 'ecs.eu-central-1.aliyuncs.com', 1),
('aliyun', 'eu-west-1', '英国(伦敦)', 'ecs.eu-west-1.aliyuncs.com', 1),
('aliyun', 'us-west-1', '美国(硅谷)', 'ecs.us-west-1.aliyuncs.com', 1),
('aliyun', 'us-east-1', '美国(弗吉尼亚)', 'ecs.us-east-1.aliyuncs.com', 1),
('aliyun', 'me-east-1', '阿联酋(迪拜)', 'ecs.me-east-1.aliyuncs.com', 1),
('aliyun', 'me-central-1', '沙特(利雅得)', 'ecs.me-central-1.aliyuncs.com', 1),
('aliyun', 'cn-qingdao', '华北1(青岛)', 'ecs.cn-qingdao.aliyuncs.com', 1);

INSERT IGNORE INTO `cloud_regions` (`platform`, `region_id`, `region_name`, `region_endpoint`, `enabled`) VALUES
('tencent', 'ap-guangzhou', '华南地区(广州)', 'cvm.ap-guangzhou.tencentcloudapi.com', 1),
('tencent', 'ap-shenzhen-fsi', '华南地区(深圳金融)', 'cvm.ap-shenzhen-fsi.tencentcloudapi.com', 1),
('tencent', 'ap-shanghai', '华东地区(上海)', 'cvm.ap-shanghai.tencentcloudapi.com', 1),
('tencent', 'ap-shanghai-fsi', '华东地区(上海金融)', 'cvm.ap-shanghai-fsi.tencentcloudapi.com', 1),
('tencent', 'ap-nanjing', '华东地区(南京)', 'cvm.ap-nanjing.tencentcloudapi.com', 1),
('tencent', 'ap-beijing', '华北地区(北京)', 'cvm.ap-beijing.tencentcloudapi.com', 1),
('tencent', 'ap-beijing-fsi', '华北地区(北京金融)', 'cvm.ap-beijing-fsi.tencentcloudapi.com', 1),
('tencent', 'ap-chengdu', '西南地区(成都)', 'cvm.ap-chengdu.tencentcloudapi.com', 1),
('tencent', 'ap-chongqing', '西南地区(重庆)', 'cvm.ap-chongqing.tencentcloudapi.com', 1),
('tencent', 'ap-hongkong', '港澳台地区(香港)', 'cvm.ap-hongkong.tencentcloudapi.com', 1),
('tencent', 'ap-taipei', '港澳台地区(台北)', 'cvm.ap-taipei.tencentcloudapi.com', 1),
('tencent', 'ap-singapore', '亚太东南(新加坡)', 'cvm.ap-singapore.tencentcloudapi.com', 1),
('tencent', 'ap-bangkok', '亚太东南(曼谷)', 'cvm.ap-bangkok.tencentcloudapi.com', 1),
('tencent', 'ap-jakarta', '亚太东南(雅加达)', 'cvm.ap-jakarta.tencentcloudapi.com', 1),
('tencent', 'ap-mumbai', '亚太南部(孟买)', 'cvm.ap-mumbai.tencentcloudapi.com', 1),
('tencent', 'ap-seoul', '亚太东北(首尔)', 'cvm.ap-seoul.tencentcloudapi.com', 1),
('tencent', 'ap-tokyo', '亚太东北(东京)', 'cvm.ap-tokyo.tencentcloudapi.com', 1),
('tencent', 'na-siliconvalley', '美国西部(硅谷)', 'cvm.na-siliconvalley.tencentcloudapi.com', 1),
('tencent', 'na-ashburn', '美国东部(弗吉尼亚)', 'cvm.na-ashburn.tencentcloudapi.com', 1),
('tencent', 'na-toronto', '北美地区(多伦多)', 'cvm.na-toronto.tencentcloudapi.com', 1),
('tencent', 'sa-saopaulo', '南美地区(圣保罗)', 'cvm.sa-saopaulo.tencentcloudapi.com', 1),
('tencent', 'eu-frankfurt', '欧洲地区(法兰克福)', 'cvm.eu-frankfurt.tencentcloudapi.com', 1),
('tencent', 'eu-moscow', '欧洲地区(莫斯科)', 'cvm.eu-moscow.tencentcloudapi.com', 1),
('tencent', 'me-saudi-arabia', '中东地区(利雅得)', 'cvm.me-saudi-arabia.tencentcloudapi.com', 1);

INSERT IGNORE INTO `cloud_regions` (`platform`, `region_id`, `region_name`, `region_endpoint`, `enabled`) VALUES
('aws', 'us-east-1', '美国东部(弗吉尼亚)', 'ec2.us-east-1.amazonaws.com', 1),
('aws', 'us-east-2', '美国东部(俄亥俄)', 'ec2.us-east-2.amazonaws.com', 1),
('aws', 'us-west-1', '美国西部(加利福尼亚)', 'ec2.us-west-1.amazonaws.com', 1),
('aws', 'us-west-2', '美国西部(俄勒冈)', 'ec2.us-west-2.amazonaws.com', 1),
('aws', 'cn-northwest-1', '中国(宁夏)', 'ec2.cn-northwest-1.amazonaws.com.cn', 1),
('aws', 'af-south-1', '非洲(开普敦)', 'ec2.af-south-1.amazonaws.com', 1),
('aws', 'ap-east-1', '亚太地区(香港)', 'ec2.ap-east-1.amazonaws.com', 1),
('aws', 'ap-south-1', '亚太地区(孟买)', 'ec2.ap-south-1.amazonaws.com', 1),
('aws', 'ap-south-2', '亚太地区(海得拉巴)', 'ec2.ap-south-2.amazonaws.com', 1),
('aws', 'ap-northeast-1', '亚太地区(东京)', 'ec2.ap-northeast-1.amazonaws.com', 1),
('aws', 'ap-northeast-2', '亚太地区(首尔)', 'ec2.ap-northeast-2.amazonaws.com', 1),
('aws', 'ap-northeast-3', '亚太地区(大阪)', 'ec2.ap-northeast-3.amazonaws.com', 1),
('aws', 'ap-southeast-1', '亚太地区(新加坡)', 'ec2.ap-southeast-1.amazonaws.com', 1),
('aws', 'ap-southeast-2', '亚太地区(悉尼)', 'ec2.ap-southeast-2.amazonaws.com', 1),
('aws', 'ap-southeast-3', '亚太地区(雅加达)', 'ec2.ap-southeast-3.amazonaws.com', 1),
('aws', 'ap-southeast-4', '亚太地区(墨尔本)', 'ec2.ap-southeast-4.amazonaws.com', 1),
('aws', 'ca-central-1', '加拿大(中部)', 'ec2.ca-central-1.amazonaws.com', 1),
('aws', 'eu-central-1', '欧洲(法兰克福)', 'ec2.eu-central-1.amazonaws.com', 1),
('aws', 'eu-central-2', '欧洲(苏黎世)', 'ec2.eu-central-2.amazonaws.com', 1),
('aws', 'eu-west-1', '欧洲(爱尔兰)', 'ec2.eu-west-1.amazonaws.com', 1),
('aws', 'eu-west-2', '欧洲(伦敦)', 'ec2.eu-west-2.amazonaws.com', 1),
('aws', 'eu-west-3', '欧洲(巴黎)', 'ec2.eu-west-3.amazonaws.com', 1),
('aws', 'eu-north-1', '欧洲(斯德哥尔摩)', 'ec2.eu-north-1.amazonaws.com', 1),
('aws', 'eu-south-1', '欧洲(米兰)', 'ec2.eu-south-1.amazonaws.com', 1),
('aws', 'eu-south-2', '欧洲(西班牙)', 'ec2.eu-south-2.amazonaws.com', 1),
('aws', 'me-south-1', '中东(巴林)', 'ec2.me-south-1.amazonaws.com', 1),
('aws', 'me-central-1', '中东(阿联酋)', 'ec2.me-central-1.amazonaws.com', 1),
('aws', 'sa-east-1', '南美洲(圣保罗)', 'ec2.sa-east-1.amazonaws.com', 1);

-- ===== 堡垒机（SSH资产管理） =====

-- SSH凭证表
CREATE TABLE IF NOT EXISTS `bastion_credentials` (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(100) NOT NULL COMMENT '凭证别名',
    `auth_type` VARCHAR(20) NOT NULL DEFAULT 'password' COMMENT '认证方式: password / private_key',
    `username` VARCHAR(100) NOT NULL COMMENT 'SSH用户名',
    `password` VARCHAR(500) NOT NULL DEFAULT '' COMMENT 'SSH密码（AES加密）',
    `private_key` TEXT COMMENT 'SSH私钥（AES加密）',
    `passphrase` VARCHAR(200) NOT NULL DEFAULT '' COMMENT '私钥密码（AES加密）',
    `created_by` INT NOT NULL DEFAULT 0 COMMENT '创建者用户ID',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='SSH凭证表';

-- 资产授权绑定表（ECS实例 → 用户组 → 凭证）
-- 【已废弃】拆分为 bastion_group_assets + bastion_group_credentials 两张独立表
-- CREATE TABLE IF NOT EXISTS `bastion_asset_bindings` (...);

-- 用户组资产表（用户组能访问哪些ECS）
CREATE TABLE IF NOT EXISTS `bastion_group_assets` (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `group_id` INT UNSIGNED NOT NULL COMMENT '用户组ID',
    `ecs_id` BIGINT UNSIGNED NOT NULL COMMENT 'ECS实例ID(ecs_instances.id)',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_group_ecs` (`group_id`, `ecs_id`),
    INDEX `idx_group_id` (`group_id`),
    INDEX `idx_ecs_id` (`ecs_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户组资产授权表';

-- 用户组凭证表（用户组能使用哪些SSH凭证）
CREATE TABLE IF NOT EXISTS `bastion_group_credentials` (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `group_id` INT UNSIGNED NOT NULL COMMENT '用户组ID',
    `credential_id` INT UNSIGNED NOT NULL COMMENT '凭证ID',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_group_cred` (`group_id`, `credential_id`),
    INDEX `idx_group_id` (`group_id`),
    INDEX `idx_credential_id` (`credential_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户组凭证授权表';

-- DNS可管理域名表（来自云平台 DescribeDomains API）
CREATE TABLE IF NOT EXISTS `dns_domains` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `account_id` VARCHAR(50) NOT NULL COMMENT '关联 cloud_accounts.account_id',
    `platform` VARCHAR(50) NOT NULL COMMENT '云平台',
    `account_name` VARCHAR(100) NOT NULL COMMENT '账号名称',
    `domain_name` VARCHAR(255) NOT NULL COMMENT '域名（唯一）',
    `ali_domain` TINYINT(1) DEFAULT 0 COMMENT '是否使用当前账号的NS',
    `starmark` TINYINT(1) DEFAULT 0 COMMENT '是否为付费版DNS',
    `record_count` INT DEFAULT 0 COMMENT '解析记录数',
    `dns_servers` VARCHAR(500) DEFAULT NULL COMMENT 'DNS服务器列表',
    `create_time` DATETIME DEFAULT NULL COMMENT '域名创建时间',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '记录创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '记录更新时间',
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_account_domain` (`account_id`, `domain_name`),
    INDEX `idx_account_id` (`account_id`),
    INDEX `idx_platform` (`platform`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='DNS可管理域名表';

-- 堡垒机用户组表
CREATE TABLE IF NOT EXISTS `user_groups` (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(100) NOT NULL COMMENT '用户组名称',
    `description` VARCHAR(500) DEFAULT NULL COMMENT '描述',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='堡垒机用户组表';

-- 堡垒机用户组成员表
CREATE TABLE IF NOT EXISTS `user_group_members` (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `group_id` INT UNSIGNED NOT NULL COMMENT '用户组ID',
    `user_id` INT UNSIGNED NOT NULL COMMENT '用户ID',
    PRIMARY KEY (`id`),
    INDEX `idx_group_id` (`group_id`),
    INDEX `idx_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='堡垒机用户组成员表';

-- CDN加速域名表（阿里云CDN / AWS CloudFront 同步，整账号先删后插刷新）
CREATE TABLE IF NOT EXISTS `cdn_domains` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `account_id` VARCHAR(50) NOT NULL COMMENT '关联 cloud_accounts.account_id',
    `platform` VARCHAR(20) NOT NULL COMMENT '云平台: aliyun/aws',
    `account_name` VARCHAR(100) NOT NULL COMMENT '账号名称',
    `domain_name` VARCHAR(200) NOT NULL COMMENT '加速域名（用户访问的域名）',
    `cname` VARCHAR(200) DEFAULT NULL COMMENT '平台分配的加速域名（阿里云 xxx.w.kunlunar.com / AWS xxx.cloudfront.net）',
    `product` VARCHAR(20) NOT NULL DEFAULT 'cdn' COMMENT '产品类型: cdn=CDN / dcdn=全站加速（阿里云独有）',
    `status` VARCHAR(20) NOT NULL DEFAULT '' COMMENT '运行状态: 运行中/配置中/已停止等',
    `biz_type` VARCHAR(50) DEFAULT NULL COMMENT '业务类型（阿里云: 图片小文件等）',
    `origin_domain` VARCHAR(500) DEFAULT NULL COMMENT '源站地址（回源域名/IP）',
    `https_on` TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否开启HTTPS（由证书状态赋值）',
    `cert_name` VARCHAR(200) DEFAULT NULL COMMENT '绑定证书名称（阿里云）',
    `cert_type` VARCHAR(50) DEFAULT NULL COMMENT '证书类型：free/paid 等（阿里云）',
    `cert_expire_time` VARCHAR(50) DEFAULT NULL COMMENT '证书到期时间（原始字符串）',
    `aliases` VARCHAR(1000) DEFAULT NULL COMMENT 'AWS CloudFront 全部备用域名（逗号分隔）',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '记录创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '记录更新时间',
    PRIMARY KEY (`id`),
    INDEX `idx_account_id` (`account_id`),
    INDEX `idx_platform` (`platform`),
    INDEX `idx_domain_name` (`domain_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='CDN加速域名表';

-- ============================================================
-- 手动迁移：若 cdn_domains 表已存在（升级场景），新建库无需执行本段。
-- 请手动执行以下 ALTER 新增证书字段（去掉行首 -- 注释后执行）：
-- ============================================================
-- ALTER TABLE `cdn_domains`
--     ADD COLUMN `cert_name` VARCHAR(200) DEFAULT NULL COMMENT '绑定证书名称（阿里云）',
--     ADD COLUMN `cert_type` VARCHAR(50) DEFAULT NULL COMMENT '证书类型：free/paid 等（阿里云）',
--     ADD COLUMN `cert_expire_time` VARCHAR(50) DEFAULT NULL COMMENT '证书到期时间（原始字符串）';

-- 登录尝试记录表（频率限制用，验证码错误不计入，由外层拦截）
CREATE TABLE IF NOT EXISTS `login_attempts` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `ip` VARCHAR(64) NOT NULL COMMENT '请求来源IP',
    `username` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '尝试的用户名',
    `success` TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否登录成功',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '尝试时间',
    PRIMARY KEY (`id`),
    INDEX `idx_ip` (`ip`),
    INDEX `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='登录尝试记录表';

-- ===== 批量命令/脚本执行（堡垒机复用 ECS 资产与 SSH 凭证） =====

-- 批量执行任务表（含脚本明文，用于操作审计）
CREATE TABLE IF NOT EXISTS `batch_tasks` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `title` VARCHAR(200) NOT NULL DEFAULT '' COMMENT '任务标题',
    `script` MEDIUMTEXT NOT NULL COMMENT '执行的命令或脚本内容（审计留痕）',
    `shell` VARCHAR(20) NOT NULL DEFAULT 'bash' COMMENT '解释器 bash/sh/python3/python',
    `network` VARCHAR(20) NOT NULL DEFAULT 'public' COMMENT '连接网络 public/eip/private',
    `credential_id` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '使用的SSH凭证ID',
    `credential_name` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '凭证名称快照',
    `concurrency` INT NOT NULL DEFAULT 5 COMMENT '实际并发数',
    `timeout_sec` INT NOT NULL DEFAULT 300 COMMENT '单台执行超时（秒）',
    `status` VARCHAR(20) NOT NULL DEFAULT 'running' COMMENT 'running/succeeded/partial/failed/canceled',
    `total` INT NOT NULL DEFAULT 0 COMMENT '目标主机数',
    `succeeded` INT NOT NULL DEFAULT 0 COMMENT '成功台数',
    `failed` INT NOT NULL DEFAULT 0 COMMENT '失败台数',
    `created_by` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '发起人用户ID',
    `created_name` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '发起人用户名快照',
    `client_ip` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '发起人来源IP',
    `err_msg` VARCHAR(1000) NOT NULL DEFAULT '' COMMENT '任务级错误信息',
    `started_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '开始时间',
    `finished_at` DATETIME DEFAULT NULL COMMENT '结束时间',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    INDEX `idx_status` (`status`),
    INDEX `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='批量执行任务表';

-- 批量执行单台结果表
CREATE TABLE IF NOT EXISTS `batch_task_results` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `task_id` BIGINT UNSIGNED NOT NULL COMMENT '关联 batch_tasks.id',
    `ecs_id` BIGINT UNSIGNED NOT NULL COMMENT '关联 ecs_instances.id',
    `asset_name` VARCHAR(200) NOT NULL DEFAULT '' COMMENT '资产名称快照',
    `host` VARCHAR(128) NOT NULL DEFAULT '' COMMENT '实际连接的IP',
    `port` INT NOT NULL DEFAULT 22 COMMENT '实际连接的SSH端口',
    `status` VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT 'pending/running/success/failed/canceled',
    `exit_code` INT DEFAULT NULL COMMENT '远端退出码，NULL 表示未取得',
    `stdout` TEXT COMMENT '标准输出（截断后）',
    `stderr` TEXT COMMENT '标准错误（截断后）',
    `err_msg` VARCHAR(1000) NOT NULL DEFAULT '' COMMENT '连接或执行错误',
    `duration_ms` BIGINT NOT NULL DEFAULT 0 COMMENT '耗时（毫秒）',
    `started_at` DATETIME DEFAULT NULL COMMENT '开始时间',
    `finished_at` DATETIME DEFAULT NULL COMMENT '结束时间',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    INDEX `idx_task_id` (`task_id`),
    INDEX `idx_ecs_id` (`ecs_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='批量执行单台结果表';