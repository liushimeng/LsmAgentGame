#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""gen_batch.py —— 批量生成新人物卡（Schema v1.0）

从 scratch 生成符合 Schema v1.0 的卡片，写入五层目录树。
支持多 L1 域、多 L2 细分、多职业族并行生成。

用法:
    python3 gen_batch.py --out <玩家职业设计目录> \
        --l1 E,I,S,B,F \
        --count 500 \
        --start-id 9002251 \
        --batch-tag v4.1

与 build_cards.py 共享 lib_derive.py / lib_cells.py / icg_l2.py / l1_rules.py，
输出格式完全一致（YAML 头部 + 正文 9 节），可直接汇入现有知识库。
"""
import argparse
import hashlib
import json
import os
import random
import re
import sys
import time

import yaml

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lib_derive as D
import icg_l2
from l1_rules import L1_NAMES

# ── 共享常量 ────────────────────────────────────────────────────
l2i = icg_l2.l2_index()
ALL_L2 = icg_l2.all_l2()

# 真实中国城市（按地区档）
CITIES = {
    'CN-N-华北': ['北京', '天津', '石家庄', '太原', '呼和浩特', '唐山', '保定', '邯郸'],
    'CN-NE-东北': ['沈阳', '大连', '哈尔滨', '长春', '大庆', '吉林'],
    'CN-E-华东': ['上海', '南京', '苏州', '杭州', '宁波', '合肥', '福州', '厦门', '济南', '青岛', '南昌'],
    'CN-C-华中': ['郑州', '武汉', '长沙', '洛阳', '襄阳', '宜昌'],
    'CN-S-华南': ['广州', '深圳', '佛山', '东莞', '南宁', '海口', '珠海', '惠州'],
    'CN-SW-西南': ['重庆', '成都', '贵阳', '昆明', '绵阳', '宜宾'],
    'CN-NW-西北': ['西安', '兰州', '西宁', '银川', '乌鲁木齐', '咸阳'],
    'OV-海外': ['香港', '澳门', '台北', '新加坡', '东京', '悉尼', '温哥华'],
}

PROVINCES = {
    '河北': 'CN-N-华北', '山西': 'CN-N-华北', '内蒙古': 'CN-N-华北',
    '辽宁': 'CN-NE-东北', '吉林': 'CN-NE-东北', '黑龙江': 'CN-NE-东北',
    '江苏': 'CN-E-华东', '浙江': 'CN-E-华东', '安徽': 'CN-E-华东',
    '福建': 'CN-E-华东', '江西': 'CN-E-华东', '山东': 'CN-E-华东',
    '河南': 'CN-C-华中', '湖北': 'CN-C-华中', '湖南': 'CN-C-华中',
    '广东': 'CN-S-华南', '广西': 'CN-S-华南', '海南': 'CN-S-华南',
    '四川': 'CN-SW-西南', '贵州': 'CN-SW-西南', '云南': 'CN-SW-西南',
    '陕西': 'CN-NW-西北', '甘肃': 'CN-NW-西北', '青海': 'CN-NW-西北',
    '宁夏': 'CN-NW-西北', '新疆': 'CN-NW-西北',
}

# 姓氏池
SURNAMES_M = ['王', '李', '张', '刘', '陈', '杨', '赵', '黄', '周', '吴', '徐', '孙', '胡', '朱', '高',
              '林', '何', '郭', '马', '罗', '梁', '宋', '郑', '谢', '韩', '唐', '冯', '于', '董', '萧',
              '程', '曹', '袁', '邓', '许', '傅', '沈', '曾', '彭', '吕', '苏', '卢', '蒋', '蔡', '贾',
              '丁', '魏', '薛', '叶', '阎', '余', '潘', '杜', '戴', '夏', '钟', '汪', '田', '任', '姜',
              '范', '方', '石', '姚', '谭', '廖', '邹', '熊', '金', '陆', '郝', '孔', '白', '崔', '康']
SURNAMES_F = SURNAMES_M
GIVEN_M = list('强军伟勇刚磊涛峰波辉鹏飞龙虎彪斌杰亮明华建国志民永康健铁钢山河海川兵武雄豪铭鑫栋梁坚毅承旭阳锋航帆骏骐腾源洲洋浩宇轩宸昊煜烨燊垚焱淼猛超越凯旋仁信义礼智忠孝勤俭谦恭')
GIVEN_F = list('兰婷娟秀慧芳燕玲莉娜静丽敏雪梅萍红霞珍琴婉妍媛瑶瑾琪珊琳琦莹蕊薇蕾蓉菲萱芸茜茵荷莲怡悦欣晴岚心月昕晞晗曦暖妤姝娴婵婧婕姣娅娴嫣彤茹蓓菁菡菱菊桃樱棠柔妙')

# ── 每个 L2 的真实职业池 ──────────────────────────────────────
# 纯数据定义：(职业名, 就业形态, 收入中位数, 收入波动, [证书], [健康风险], [压力源])
# 所有条目在 _build_occ_pool() 中转为 tuple

_RAW_OCC = {
    'E01': [
        ["锯木工","全职",5500,2000,["木材防腐证"],["粉尘吸入","噪声"],["订单波动","环保限产"]],
        ["人造板操作工","全职",6000,1500,[],["甲醛暴露","噪声"],["自动化替代","原料涨价"]],
        ["木材采购员","全职",7500,3000,["物流师"],["久站出差"],["供应链断裂","进口关税"]],
        ["木工机械操作工","全职",6500,1800,[],["噪声","割伤风险"],["技术升级","技工短缺"]],
    ],
    'E02': [
        ["家具设计师","全职",9000,4000,["室内设计师证"],["久坐","视力下降"],["甲方反复修改","项目回款慢"]],
        ["沙发缝纫工","全职",5500,1200,[],["久坐","腰椎"],["订单季节性","计件压力"]],
        ["定制家具测量师","全职",8000,3500,[],["出差奔波"],["测量出错赔偿","工期紧张"]],
        ["家具喷漆工","全职",7000,2000,[],["化学品暴露","呼吸道"],["环保督察","VOC治理成本"]],
    ],
    'E03': [
        ["全屋定制设计师","全职",10000,5000,["室内设计师证"],["久坐熬夜"],["签单压力","方案返工"]],
        ["家居销售顾问","全职",7000,4000,[],["久站"],["业绩指标","客户投诉"]],
        ["软装搭配师","全职",8500,3500,[],[],["审美差异","供应商断货"]],
    ],
    'E04': [
        ["造纸工","全职",6000,1500,[],["噪声","湿热","化学品"],["环保限产","原料波动"]],
        ["纸箱印刷机长","全职",7500,2000,[],["噪声","油墨暴露"],["订单不足","色差追责"]],
        ["纸品品控员","全职",6500,1500,["质量体系内审员"],["奔波"],["客户退货","标准升级"]],
    ],
    'E05': [
        ["印刷机长","全职",8000,2500,[],["噪声","油墨暴露"],["色差追责","交期紧张"]],
        ["包装设计师","全职",9000,3500,["平面设计师证"],["久坐"],["反复改稿","创意枯竭"]],
        ["印前制版员","全职",7000,1800,[],["化学品暴露"],["数字化替代","技工短缺"]],
        ["装订工人","全职",5000,1000,[],["久坐","腰椎"],["计件工资","订单波动"]],
    ],
    'E06': [
        ["竹编师傅","个体经营",5000,3000,[],["手部劳损"],["传承断代","原料稀缺"]],
        ["藤艺设计师","自由职业",7000,4000,[],[],["市场小众","出口波动"]],
    ],
    'E07': [
        ["木雕师","自由职业",8000,5000,[],["粉尘吸入","手部劳损"],["学艺周期长","市场小众"]],
        ["红木家具修复师","自由职业",12000,6000,[],["粉尘"],["人才稀缺","真品鉴定风险"]],
    ],
    'I01': [
        ["IC设计工程师","全职",25000,8000,["集成电路设计师证"],["久坐","视力下降","熬夜"],["流片失败风险","项目节点紧张"]],
        ["芯片验证工程师","全职",22000,7000,[],["久坐熬夜"],["BUG追责","版本迭代快"]],
        ["模拟电路设计师","全职",28000,9000,[],["久坐"],["人才稀缺","技术壁垒高"]],
        ["数字后端工程师","全职",24000,8000,[],["久坐熬夜"],["时序收敛压力","先进工艺挑战"]],
    ],
    'I02': [
        ["封装测试工程师","全职",15000,5000,[],["化学品暴露","洁净室"],["良率压力","设备故障"]],
        ["SMT操作工","全职",6500,1500,[],["辐射","眼部疲劳"],["高速运转","焊接质量追责"]],
        ["芯片测试技术员","全职",8000,2000,[],["久坐"],["测试覆盖率","不良品溢出"]],
    ],
    'I03': [
        ["电子元器件销售","全职",10000,6000,[],["应酬"],["价格战","库存跌价"]],
        ["FAE现场应用工程师","全职",16000,5000,[],["出差奔波"],["客户投诉","技术支持压力"]],
        ["电子元器件品控","全职",9000,2500,["质量体系内审员"],["显微镜用眼"],["来料不良","供应商管理"]],
    ],
    'I04': [
        ["PCB设计工程师","全职",14000,4000,[],["久坐"],["布线复杂度","EMC整改"]],
        ["电路板焊接工","全职",6000,1500,[],["铅烟暴露","眼部疲劳"],["精密焊接要求","产能压力"]],
        ["FPC工程师","全职",12000,3500,[],["化学品暴露"],["弯折可靠性","材料国产化"]],
    ],
    'I05': [
        ["显示面板工程师","全职",18000,6000,[],["洁净室","视力下降"],["良率爬坡","设备调试"]],
        ["光学薄膜技术员","全职",9000,2500,[],["洁净室"],["膜材缺陷","洁净度要求"]],
    ],
    'I06': [
        ["计量校准工程师","全职",11000,3000,["注册计量师"],["现场奔波"],["量值溯源要求","客户投诉"]],
        ["仪器仪表维修工","全职",8500,2500,[],["出差","现场环境复杂"],["故障紧急抢修","备件等待"]],
        ["自动化仪表工程师","全职",13000,4000,[],["出差"],["联锁逻辑复杂","现场干扰"]],
    ],
    'I07': [
        ["检测工程师","全职",10000,3000,["CNAS评审员"],["实验室化学品"],["数据准确性","标准更新"]],
        ["认证工程师","全职",12000,3500,["CCC审核员"],["出差"],["标准变化","工厂审核"]],
    ],
    'I08': [
        ["射频工程师","全职",20000,6000,[],["辐射暴露"],["信号干扰","频段合规"]],
        ["天线设计师","全职",18000,5000,[],["户外测试"],["仿真与实测差距","小型化要求"]],
        ["通信技术员","全职",9000,2500,["通信工程师证"],["登高作业"],["基站维护紧急","暴风雨抢修"]],
    ],
    'I09': [
        ["嵌入式软件工程师","全职",18000,5000,[],["久坐熬夜"],["硬件依赖调试","实时性要求"]],
        ["硬件工程师","全职",16000,5000,[],["焊接烟雾"],["原理图错误返修","BOM成本"]],
        ["计算机维修技师","全职",7000,2000,["计算机维修工"],[],["客户着急","配件假货"]],
    ],
    'S01': [
        ["生物实验室研究员","全职",12000,4000,["实验动物操作证"],["化学品暴露","生物危害"],["论文压力","实验失败"]],
        ["化学分析员","全职",9000,2500,[],["化学品暴露"],["数据重复性","标准品过期"]],
        ["物理实验员","全职",10000,3000,[],["辐射暴露"],["设备排期紧张","实验精度要求"]],
    ],
    'S02': [
        ["机械研发工程师","全职",15000,5000,[],["车间噪声"],["项目节点","图纸错误追溯"]],
        ["电气工程师","全职",14000,4500,["注册电气工程师"],["现场带电作业"],["安全责任","图纸变更频繁"]],
        ["结构工程师","全职",16000,5000,["一级注册结构师"],["久坐"],["计算书复核","甲方压缩周期"]],
    ],
    'S03': [
        ["第三方检测技术员","全职",8000,2000,["检验检测员证"],["化学品/粉尘"],["样品积压","数据准确性追责"]],
        ["认证审核员","全职",11000,4000,["ISO审核员"],["出差奔波"],["工厂不配合","标准更新快"]],
    ],
    'S04': [
        ["测绘工程师","全职",10000,3500,["注册测绘师"],["户外暴晒","登高"],["通视条件差","控制点破坏"]],
        ["GIS数据分析师","全职",12000,3000,[],["久坐"],["数据更新滞后","坐标系转换"]],
    ],
    'S05': [
        ["气象观测员","全职",7500,1500,[],["野外值守","极端天气"],["台站偏远","数据缺测"]],
        ["海洋观测技术员","全职",9000,2500,[],["出海颠簸","晕船"],["海况恶劣","设备腐蚀"]],
    ],
    'S06': [
        ["建筑设计工程师","全职",16000,6000,["一级注册建筑师"],["通宵赶图"],["甲方反复修改","强条复核"]],
        ["城市规划师","全职",14000,4000,["注册规划师"],["久坐"],["规划调整","公众参与压力"]],
    ],
    'S07': [
        ["试验技术员","全职",8500,2000,[],["噪声","试车危险"],["试验排期紧","数据采集失败"]],
        ["中试工程师","全职",13000,3500,[],["车间环境"],["工艺放大失败","良品率"]],
    ],
    'S08': [
        ["科研管理人员","全职",12000,3000,[],["久坐"],["项目经费审计","成果转化考核"]],
        ["技术转移经理","全职",15000,5000,[],["谈判压力"],["专利估值难","转化周期长"]],
    ],
    'B01': [
        ["煤矿井下采煤工","全职",9000,3000,["煤矿安全证"],["瓦斯暴露","粉尘","冒顶风险"],["安全压力大","井下高温"]],
        ["选煤厂操作工","全职",6500,1500,[],["粉尘","噪声"],["订单波动","环保要求"]],
        ["矿山安全员","全职",8500,2500,["注册安全工程师"],["井下环境"],["安全责任重大","隐患排查压力"]],
    ],
    'B02': [
        ["钻井技术员","全职",12000,4000,[],["野外值守","噪声"],["井控安全","井漏井喷"]],
        ["采油工","全职",8000,2500,[],["原油化学品","野外孤独"],["产量递减","夜班值守"]],
        ["压裂工程师","全职",15000,5000,[],["野外","噪声"],["施工安全","设备故障"]],
    ],
    'B03': [
        ["矿山爆破工","全职",10000,3000,["爆破作业证"],["爆炸风险","粉尘"],["爆破安全","民爆品管理"]],
        ["矿山提升机操作工","全职",7000,1800,[],["噪声","久坐"],["提升安全","设备故障应急"]],
        ["矿用卡车司机","全职",9500,2500,["矿用车驾照"],["颠簸","噪声"],["运输效率","矿区道路危险"]],
    ],
    'B04': [
        ["石英砂矿加工员","全职",6000,1500,[],["粉尘"],["粉尘治理","产品价格波动"]],
        ["石墨矿采选工","全职",7000,2000,[],["粉尘","噪声"],["环保限产","石墨价格波动"]],
    ],
    'B05': [
        ["炼钢工","全职",8000,2500,[],["高温辐射","噪声"],["钢水安全","能耗指标"]],
        ["轧钢操作工","全职",7500,2000,[],["高温","噪声"],["轧制精度","换辊时间压缩"]],
        ["钢铁质检员","全职",7000,1800,[],["高温"],["质量异议","标准升级"]],
        ["钢铁销售员","全职",9000,5000,[],["应酬"],["价格波动","库存风险"]],
    ],
    'B06': [
        ["电解铝操作工","全职",7500,2000,[],["高温","强磁场","氟化物"],["电流效率","安全"]],
        ["铜冶炼工","全职",8000,2200,[],["高温","二氧化硫"],["环保排放","能耗双控"]],
        ["锌冶炼技术员","全职",8500,2500,[],["化学品暴露"],["工艺稳定性","回收率"]],
    ],
    'B07': [
        ["稀土萃取技术员","全职",10000,3000,[],["化学品暴露"],["萃取分离系数","环保"]],
        ["稀有金属冶金工程师","全职",14000,4000,[],["高温","稀有气体"],["工艺保密","国际价格波动"]],
    ],
    'B08': [
        ["矿井通风工程师","全职",11000,3000,["注册安全工程师"],["井下环境"],["通风系统优化","瓦斯治理"]],
        ["矿冶安全评价师","全职",12000,3500,["安全评价师"],["井下出差"],["法规更新","事故追责"]],
    ],
    'B09': [
        ["地质勘查技术员","全职",9000,3000,[],["野外暴晒","蚊虫"],["钻孔见矿率","找矿难度增大"]],
        ["钻井地质师","全职",12000,4000,[],["野外值守"],["地层对比","取芯率要求"]],
        ["测量测绘员","全职",8000,2500,[],["户外暴晒"],["控制点精度","天气影响"]],
    ],
    'B10': [
        ["废金属回收分拣员","全职",5500,1500,[],["粉尘","割伤"],["价格波动","辛苦活"]],
        ["再生资源加工技术员","全职",7500,2000,[],["粉尘","噪声"],["回收渠道不稳定","加工利润薄"]],
    ],
    'F01': [
        ["原料药操作工","全职",7000,2000,[],["化学品暴露","粉尘"],["环保压力","GMP合规"]],
        ["化学合成研究员","全职",13000,4000,[],["化学品暴露"],["合成路线失败","文献复现偏差"]],
        ["药物制剂工程师","全职",14000,4000,[],[],["一致性评价","工艺放大"]],
    ],
    'F02': [
        ["中药炮制工","全职",6500,1800,["中药炮制工证"],["高温","粉尘"],["火候把控","学徒周期长"]],
        ["中药饮片质检","全职",8000,2000,["执业药师"],[],["农残重金属标准","掺假鉴别"]],
        ["中药材种植技术员","全职",7500,2500,[],["户外暴晒"],["道地性要求","价格波动"]],
    ],
    'F03': [
        ["疫苗生产技术员","全职",10000,2500,[],["生物危害","洁净室"],["批签发合格率","生物安全"]],
        ["生物制品研究员","全职",16000,5000,[],["生物危害"],["细胞污染","项目节点"]],
        ["发酵工程师","全职",13000,3500,[],["化学品暴露"],["菌种稳定性","染菌风险"]],
    ],
    'F04': [
        ["医疗器械装配工","全职",7000,1800,[],["焊接烟雾"],["产能压力","质量追溯"]],
        ["医疗器械注册专员","全职",12000,3500,[],["久坐"],["注册标准变化","发补应对"]],
        ["IVD试剂技术员","全职",10000,3000,[],["化学品暴露"],["灵敏度特异性","批间差"]],
    ],
    'F05': [
        ["临床研究员CRA","全职",14000,5000,["GCP证"],["出差奔波"],["入组进度","数据质疑"]],
        ["临床药理研究员","全职",18000,6000,[],["实验室"],["PK/PD分析","方案设计"]],
        ["药物警戒专员","全职",13000,3500,[],["久坐"],["个例报告时效","信号检测"]],
    ],
    'F06': [
        ["医药代表","全职",12000,8000,[],["应酬"],["集采压力","指标任务"]],
        ["药店店长","全职",8000,3000,["执业药师"],["久站"],["客流下降","医保合规"]],
        ["药品验收员","全职",6500,1500,[],["久坐"],["票账货相符","近效期管理"]],
    ],
    'F07': [
        ["合成生物学研究员","全职",20000,6000,[],["实验室"],["基因线路不稳","转化效率"]],
        ["生物发酵工艺员","全职",11000,3000,[],["化学品暴露"],["代谢通路调控","纯化收率"]],
    ],
    'F08': [
        ["保健品研发员","全职",10000,3000,[],["实验室"],["配方合规","功效声称限制"]],
        ["营养师","全职",8500,3500,["注册营养师"],[],["客户依从性","口碑压力"]],
    ],
    'F09': [
        ["药品注册专员","全职",14000,4000,[],["久坐"],["法规变更","发补时限"]],
        ["GMP合规专员","全职",12000,3000,[],["出差"],["飞检应对","数据完整性"]],
    ],
    'J01': [
        ["汽车装配工","全职",7000,2000,[],["噪声","久坐"],["产能节拍","质量追溯"]],
        ["汽车零部件销售","全职",9000,5000,[],["应酬"],["价格竞争","账期压力"]],
        ["汽车焊接工","全职",8000,2500,[],["焊接烟尘","弧光"],["焊缝质量","产能压力"]],
        ["汽车涂装工","全职",7500,2000,[],["化学品暴露"],["VOC治理","漆面质量"]],
    ],
    'J02': [
        ["电池工程师","全职",18000,6000,[],["化学品暴露"],["能量密度","安全性平衡"]],
        ["电机控制工程师","全职",20000,6000,[],["电磁辐射"],["控制精度","EMC"]],
        ["电控软件工程师","全职",22000,7000,[],["久坐熬夜"],["功能安全ISO26262","OTA升级"]],
    ],
    'J03': [
        ["汽车维修技师","全职",8000,3000,["汽车维修工证"],["油污","噪声"],["技术更新快","客户投诉"]],
        ["4S店销售顾问","全职",9000,6000,[],["久站","应酬"],["指标压力","客户投诉"]],
        ["汽车定损员","全职",10000,3500,["保险公估师"],["出差","现场查勘"],["定损金额争议","骗保识别"]],
    ],
    'J04': [
        ["船舶焊接工","全职",9000,3000,["船级社焊工证"],["弧光","密闭空间"],["焊缝探伤合格率","舱室通风"]],
        ["船舶设计工程师","全职",16000,5000,[],["久坐"],["规范更新","送审退审"]],
    ],
    'J05': [
        ["无人机飞手","自由职业",10000,5000,["AOPA证"],["户外暴晒"],["空域审批","炸机风险"]],
        ["航空发动机维修员","全职",14000,4000,["CAAC维修证"],["噪声","化学品"],["适航责任","排故时间压力"]],
        ["无人机测绘操作员","全职",9000,3000,[],["户外暴晒"],["天气影响","数据处理"]],
    ],
    'J06': [
        ["轨道交通信号工程师","全职",15000,4000,[],["现场调试"],["安全苛求","故障零容忍"]],
        ["高铁检修技师","全职",10000,3000,[],["夜间作业","高压"],["天窗时间紧张","安全责任"]],
    ],
    'J07': [
        ["电动自行车维修工","个体经营",7000,3000,[],["油污"],["竞争激烈","电商冲击"]],
        ["摩托车装配工","全职",6000,1500,[],["噪声"],["新能源替代","订单不足"]],
    ],
    'J08': [
        ["二手车评估师","全职",10000,6000,["二手车鉴定评估师"],["奔波"],["车况看走眼","价格波动"]],
        ["汽车金融专员","全职",9000,5000,[],["久坐"],["贷款拒件率","逾期催收"]],
    ],
    'J09': [
        ["汽车试验工程师","全职",14000,4000,[],["试车危险","极端环境"],["试验进度","数据有效性"]],
        ["汽车碰撞仿真工程师","全职",18000,5000,[],["久坐"],["仿真精度","计算资源"]],
    ],
    'G01': [
        ["化工操作工","全职",8000,2500,["化工总控工证"],["化学品暴露","噪声"],["安全责任","DCS操作压力"]],
        ["石化技术员","全职",10000,3000,[],["化学品暴露","高温"],["装置平稳率","安全"]],
        ["炼油工程师","全职",15000,4500,[],["化学品暴露"],["收率优化","能耗指标"]],
    ],
    'G02': [
        ["精细化工技术员","全职",9000,2500,[],["化学品暴露"],["工艺稳定性","小批量切换"]],
        ["化妆品配方师","全职",12000,4000,[],["实验室"],["配方稳定性","新原料合规"]],
    ],
    'G03': [
        ["注塑成型操作工","全职",7000,2000,[],["高温","噪声"],["不良率","模具维修"]],
        ["橡胶硫化工","全职",7500,2000,[],["高温","硫化烟气"],["气泡缺陷","硫化时间控制"]],
        ["塑料制品设计师","全职",10000,3500,[],["久坐"],["模具成本","结构强度"]],
    ],
    'G04': [
        ["涂料技术员","全职",9000,3000,[],["化学品暴露","VOC"],["环保标准","配方成本"]],
        ["油墨工程师","全职",10000,3000,[],["化学品暴露"],["色彩准确性","干燥速度"]],
    ],
    'G05': [
        ["化妆品生产员","全职",6500,1500,[],["洁净室"],["GMP合规","批次稳定性"]],
        ["日化销售代表","全职",8500,4000,[],["应酬","出差"],["渠道压货","促销效果"]],
        ["香精香料调配师","全职",11000,3500,[],["化学品暴露"],["香气稳定性","天然原料稀缺"]],
    ],
    'G06': [
        ["锂电材料工程师","全职",18000,6000,[],["化学品暴露"],["材料一致性","客户认证周期"]],
        ["光伏材料技术员","全职",12000,3500,[],["化学品暴露"],["转化效率","衰减率"]],
    ],
    'G07': [
        ["光刻胶研发工程师","全职",25000,8000,[],["化学品暴露"],["纯度要求","量产一致性"]],
        ["电子特气技术员","全职",12000,3000,[],["化学品暴露"],["纯度","运输安全"]],
    ],
    'G08': [
        ["水泥工艺工程师","全职",13000,3500,[],["粉尘","噪声"],["能耗双控","错峰生产"]],
        ["混凝土技术员","全职",9000,2500,[],["粉尘","户外"],["强度达标","浇筑连续性"]],
        ["玻璃熔化工","全职",8500,2500,[],["高温辐射"],["窑炉寿命","玻璃缺陷率"]],
    ],
    'G09': [
        ["碳纤维生产技术员","全职",12000,3500,[],["化学品暴露"],["丝束稳定性","收丝率"]],
        ["复合材料工程师","全职",14000,4000,[],["化学品"],["成型工艺","无损检测"]],
    ],
    'G10': [
        ["化工环保工程师","全职",13000,3500,["注册环保工程师"],["化学品暴露"],["排放标准升级","三废处理成本"]],
        ["化工安全工程师","全职",14000,4000,["注册安全工程师"],["现场检查"],["重大危险源","应急响应"]],
    ],
    'C01': [
        ["粮油加工操作工","全职",6000,1500,[],["粉尘","噪声"],["订单季节性","黄曲霉控制"]],
        ["面粉厂技术员","全职",7500,2000,[],["粉尘"],["含灰量","出粉率"]],
    ],
    'C02': [
        ["屠宰分割工","全职",7000,2000,[],["低温","割伤风险"],["卫生标准","检疫压力"]],
        ["肉制品加工技术员","全职",8500,2500,[],["低温"],["添加剂合规","保质期测试"]],
    ],
    'C03': [
        ["乳品加工技术员","全职",8000,2500,[],["洁净区"],["微生物指标","冷链稳定"]],
        ["奶牛养殖技术员","全职",7500,2000,[],["户外","动物接触"],["奶牛单产","原奶价格"]],
    ],
    'C04': [
        ["烘焙师","全职",7000,2500,["烘焙工证"],["高温","久站"],["起早","口味稳定性"]],
        ["面条加工操作工","全职",6000,1500,[],["噪声"],["干燥温度","断条率"]],
    ],
    'C05': [
        ["酱油酿造工","全职",7000,2000,[],["高温高湿"],["发酵周期","风味一致性"]],
        ["调味品研发员","全职",10000,3000,[],["实验室"],["配方保密","成本压力"]],
    ],
    'C06': [
        ["食品质检员","全职",7000,1800,["检验工证"],["实验室"],["数据准确性","标准升级"]],
        ["糖果生产技术员","全职",7500,2000,[],["高温"],["浇注温度","包装密封性"]],
    ],
    'C07': [
        ["饮料调配技术员","全职",8000,2500,[],["洁净区"],["糖酸比稳定性","微生物控制"]],
        ["茶饮门店店长","全职",7500,3000,[],["久站"],["客流波动","加盟商管理"]],
    ],
    'C08': [
        ["啤酒酿造工","全职",7500,2000,[],["噪声","低温"],["发酵温度控制","风味一致性"]],
        ["白酒勾调师","全职",12000,5000,[],["化学品暴露"],["口感一致性","基酒稀缺"]],
    ],
    'C09': [
        ["预制菜研发员","全职",11000,3500,[],["实验室"],["口感复原度","锁鲜技术"]],
        ["中央厨房生产主管","全职",9000,2500,[],["高温"],["出餐效率","食品安全"]],
    ],
    'C10': [
        ["食品研发工程师","全职",13000,4000,[],["实验室"],["新品上市周期","成本对标"]],
        ["感官评价员","全职",8000,2000,[],["味觉疲劳"],["主观偏差","代表性"]],
    ],
    'C11': [
        ["烟草分级员","全职",9000,2500,[],["粉尘"],["等级判定一致性","国家标准变化"]],
        ["卷烟厂操作工","全职",8500,2000,[],["噪声","粉尘"],["产能目标","设备故障"]],
    ],
    'C12': [
        ["冷链物流管理员","全职",8500,2500,[],["低温环境"],["温度断链风险","设备故障"]],
        ["速冻食品操作工","全职",6500,1500,[],["低温"],["产能压力","质量追溯"]],
    ],
    'D01': [
        ["纺织挡车工","全职",6000,1500,[],["噪声","粉尘"],["纱线断头率","质量索赔"]],
        ["印染调色技术员","全职",8500,2500,[],["化学品暴露","高温高湿"],["色差追责","环保排放"]],
    ],
    'D02': [
        ["服装缝纫工","全职",5500,1500,[],["久坐","视力下降"],["计件工资","订单季节性"]],
        ["服装质检员","全职",6500,1500,[],["久坐"],["客户退货","标准升级"]],
    ],
    'D03': [
        ["服装设计师","全职",10000,4500,["服装设计师证"],["久坐熬夜"],["爆款预测","改版压力"]],
        ["服装制版师","全职",9000,3000,[],["久坐"],["版型还原度","缩水率"]],
    ],
    'D04': [
        ["鞋样设计师","全职",9000,3500,[],["久坐"],["舒适度与外观平衡","开模成本"]],
        ["皮具制作工","全职",7000,2500,[],["化学品暴露"],["手工效率","皮料成本"]],
    ],
    'D05': [
        ["家纺设计师","全职",9500,3500,[],["久坐"],["花型侵权","库存风险"]],
        ["产业用纺织品工程师","全职",12000,3500,[],[],["技术壁垒","客户认证"]],
    ],
    'D06': [
        ["服装陈列师","全职",7500,2500,[],["久站"],["换季调整","坪效指标"]],
        ["服装买手","全职",10000,5000,[],["出差奔波"],["选品失误库存","流行趋势误判"]],
    ],
    'D07': [
        ["纺织服装外贸跟单","全职",8000,3000,["单证员证"],["久坐熬夜"],["交期延误","汇率波动"]],
        ["纺织检测工程师","全职",9000,2500,[],["实验室"],["检测标准更新","数据准确"]],
    ],
    'D08': [
        ["时尚品牌主理人","个体经营",12000,8000,[],[],["库存积压","品牌定位"]],
        ["服装搭配师","全职",8000,3500,[],[],["客户审美差异","同行竞争"]],
    ],
}

def _build_occ_pool():
    """将 _RAW_OCC 转为 tuple 格式的 OCC_POOL"""
    out = {}
    for k, items in _RAW_OCC.items():
        out[k] = [tuple(it) for it in items]
    return out

# 修复上面的列表/元组混用
def _normalize_pool(raw):
    out = {}
    for k, items in raw.items():
        fixed = []
        for it in items:
            if isinstance(it, list):
                it = tuple(it)
            fixed.append(it)
        out[k] = fixed
    return out


OCC_POOL = _normalize_pool(_build_occ_pool())


def _seg(s):
    return re.sub(r'[^\w一-鿿\-]+', '-', str(s)).strip('-')[:40]


def _money(v):
    if v is None:
        return '未采集'
    try:
        v = float(v)
    except (TypeError, ValueError):
        return str(v)
    if v >= 10000:
        return '%.1f 万' % (v / 10000)
    return '%d 元' % int(v)


def _hash_unit(key, salt=''):
    h = hashlib.sha256((salt + '|' + key).encode('utf-8')).hexdigest()
    return int(h[:8], 16) / 0xFFFFFFFF


class Generator:
    def __init__(self, out_dir, start_id=9002251, batch_tag='v4.1', seed=None):
        self.out_dir = out_dir
        self.next_id = start_id
        self.batch_tag = batch_tag
        self.stats = {}
        self.rng = random.Random(seed)
        self.used_names = set()
        self.used_ids = set()

    def _new_id(self):
        nid = self.next_id
        self.next_id += 1
        return 'N%d' % nid

    def _fresh_name(self, gender):
        for _ in range(50):
            s = self.rng.choice(SURNAMES_F if gender == '女' else SURNAMES_M)
            n = ''.join(self.rng.choices(GIVEN_F if gender == '女' else GIVEN_M,
                                         k=self.rng.choice([1, 1, 2])))
            name = s + n
            if name not in self.used_names:
                self.used_names.add(name)
                return name
        return '某'

    def _pick(self, seq):
        return self.rng.choice(seq) if seq else None

    def _income_range(self, mid, swing):
        lo = max(1500, int(mid - swing))
        hi = int(mid + swing)
        return [lo, hi]

    def _savings_rate(self, inc, marital, kids, housing, age):
        """基于现实参数估算储蓄率"""
        base = 0.25
        if inc > 20000: base += 0.10
        elif inc < 6000: base -= 0.10
        if kids > 0: base -= 0.05 * kids
        if housing == '租赁': base -= 0.05
        elif housing == '自有' and age > 35: base += 0.05
        if marital == '已婚': base -= 0.03
        return max(0.02, min(0.70, base + self.rng.uniform(-0.08, 0.08)))

    def generate_one(self, l1, l2, occ_info):
        """生成单张卡片"""
        occ_name, employment, inc_mid, inc_swing, certs_pool, health_risks_pool, stress_pool = occ_info

        card = {}
        card_id = self._new_id()
        card['id'] = card_id
        card['schema_version'] = '1.0'
        card['card_type'] = 'person'
        card['richness'] = 'skeleton'
        card['source_file'] = '%s-%s-auto-gen.md' % (l2, _seg(occ_name))
        card['source_batch'] = self.batch_tag
        card['created_at'] = '2026-09-13'

        # 性别
        gender = self.rng.choice(['男', '女'])
        card['gender'] = gender
        name = self._fresh_name(gender)
        card['name'] = name

        # 年龄与阶段
        age = self.rng.choices(
            range(18, 66),
            weights=[3,4,5,5,5,5,5,5,5,5,5,5,5,5,5,5,5,4,4,4,3,3,3,3,3,2,2,2,2,2,2,2,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1],
            k=1)[0]
        card['age'] = age
        card['age_band'] = D.age_band(age)
        card['birth_year'] = 2026 - age
        card['generation'] = D.generation(card['birth_year'])

        # 城市与地区
        region = self.rng.choice(list(CITIES.keys()))
        city = self.rng.choice(CITIES[region])
        card['birth_province'] = self.rng.choice(list(PROVINCES.keys()))
        card['birth_city'] = city
        card['housing_city'] = city
        card['housing_tenure'] = self.rng.choices(
            ('自有', '租赁', '合租', '单位/保障住房', '父母产权'),
            weights=[35, 30, 15, 10, 10], k=1)[0]

        # 学历
        edu_probs = self.rng.random()
        if employment in ('全职',):
            card['education'] = self.rng.choices(
                ('本科', '大专', '硕士', '高中', '中专', '初中', '博士', 'MBA', 'EMBA'),
                weights=[30, 25, 15, 10, 8, 5, 3, 2, 2], k=1)[0]
        elif employment in ('个体经营', '自由职业', '平台就业'):
            card['education'] = self.rng.choices(
                ('高中', '大专', '本科', '硕士', '中专', '初中'),
                weights=[25, 30, 25, 10, 7, 3], k=1)[0]
        else:
            card['education'] = self.rng.choice(['本科', '大专', '硕士', '高中'])

        # 健康
        health_grade = self.rng.choices(['A', 'B', 'C'], weights=[50, 40, 10], k=1)[0]
        card['health_grade'] = health_grade
        conditions = []
        if age > 45:
            conditions += self.rng.sample(['高血压', '脂肪肝', '血糖偏高', '关节退行性变'],
                                         k=min(2, self.rng.randint(0, 2)))
        if age > 35:
            conditions += self.rng.sample(['慢性咽炎', '腰椎', '颈椎', '失眠'],
                                         k=min(1, self.rng.randint(0, 2)))
        if health_grade == 'C':
            conditions += self.rng.sample(['心脏病', '糖尿病', '严重腰椎间盘突出', '应酬肝'],
                                         k=self.rng.randint(1, 2))
        card['health_conditions'] = list(dict.fromkeys(conditions))
        risks = []
        for c in card['health_conditions']:
            risks.extend(D.derive_health_risks([c], occ_name)[0])
        for r in health_risks_pool:
            if r not in risks:
                risks.append(r)
        card['health_risks'] = risks[:4]

        # 职业
        card['industry_l1'] = l1
        card['industry_l2'] = l2
        # L3 = L2 + 01..99
        l3_num = self.rng.randint(1, 99)
        card['industry_l3'] = '%s%02d' % (l2, l3_num)
        # === v2.0 数字编号（2026-09-13）：同步产出 ===
        # 由 l1 字母 + occ_name 推 v2.0 L1 数字（拆分到 19 时按判定词）
        from v2_mapping import resolve_l1, l2_num_for, l3_num_for
        l1_num = resolve_l1(l1, occ_name, '')
        l2_num = l2_num_for(l1, l1_num, l2)
        l3_num_full = l3_num_for(l2_num, card['industry_l3'])
        card['occ_industry_num'] = l1_num
        card['occ_l2_num'] = l2_num
        card['occ_l3_num'] = l3_num_full
        # occ_id 由后续批量步骤分配（见 build_cards.py → resolve_ids）
        card['occupation'] = occ_name
        card['employment'] = employment
        card['employer'] = ('%s自营主体' % city if employment == '个体经营' else
                            '平台用工（众包/加盟）' if employment == '平台就业' else
                            '自由职业/接单' if employment == '自由职业' else
                            '%s%s单位' % (city, self.rng.choice(['国有', '民营', '合资'])))
        weekly_h = self.rng.choice([40, 44, 48, 50, 55, 60])
        card['work_intensity'] = {
            'weekly_hours': weekly_h,
            'overtime': '高' if weekly_h > 50 else ('中' if weekly_h > 44 else '低'),
            'risk': self.rng.choices(['高', '中', '低'], weights=[20, 50, 30], k=1)[0]
        }
        card['career_stage'] = D.derive_stage(occ_name)[0]

        # 收入
        inc_var = self.rng.uniform(0.7, 1.3)
        income = int(inc_mid * inc_var / 100) * 100
        income = max(3000, income)
        card['income_monthly'] = income
        card['income_range'] = self._income_range(income, int(inc_swing * inc_var))
        card['income_structure'] = self.rng.choices(
            ('固定薪资', '提成+底薪', '项目制', '季节波动', '计件', '年薪制', '合伙分红'),
            weights=[40, 20, 15, 10, 8, 5, 2], k=1)[0]
        card['income_stability'] = self.rng.choices(['高', '中', '低'], weights=[40, 40, 20], k=1)[0]
        # 家庭收入
        hh_mult = 1.0
        if self.rng.random() < 0.6:
            hh_mult = self.rng.uniform(1.3, 2.0)
        card['household_monthly'] = int(income * hh_mult / 100) * 100

        # 财务
        sr = self._savings_rate(income, '已婚' if self.rng.random() < 0.6 else '未婚',
                                0 if self.rng.random() < 0.3 else 1,
                                card['housing_tenure'], age)
        monthly_exp = int(income * (1 - sr) / 100) * 100
        card['monthly_expense'] = max(1500, monthly_exp)
        card['savings_stock'] = max(0, int(income * sr * self.rng.uniform(8, 48) / 1000) * 1000)
        card['debt_stock'] = 0
        card['savings_rate'] = round((income - card['monthly_expense']) / income, 3)

        assets = [{'type': '现金/存款', 'desc': '档案储蓄存量', 'value_cny': card['savings_stock']}]
        debts = []
        if card['housing_tenure'] == '自有' and self.rng.random() < 0.7:
            mortgage = int(self.rng.uniform(30, 180)) * 10000
            assets.append({'type': '房产', 'desc': '%s自有住房,房贷余%d万' % (city, mortgage // 10000),
                           'value_cny': int(mortgage * self.rng.uniform(2.5, 5))})
            debts.append({'type': '房贷', 'balance': mortgage, 'monthly': int(mortgage * 0.0055),
                           'source': '住房贷款'})
            card['debt_stock'] += mortgage
            card['mortgage_left'] = mortgage
            card['housing_detail'] = '%s自有住房,房贷余%d万' % (city, mortgage // 10000)
        else:
            card['housing_detail'] = '%s%s' % (city, card['housing_tenure'])
            card['mortgage_left'] = None

        if self.rng.random() < 0.15:
            car_loan = int(self.rng.uniform(5, 15)) * 10000
            debts.append({'type': '车贷', 'balance': car_loan, 'monthly': int(car_loan * 0.03),
                           'source': '汽车贷款'})
            card['debt_stock'] += car_loan
            assets.append({'type': '车辆', 'desc': '自有代步车', 'value_cny': car_loan})

        card['assets'] = assets
        card['debts'] = debts
        card['net_worth'] = card['savings_stock'] - card['debt_stock']

        # 保障
        card['certs'] = list(self.rng.sample(certs_pool, min(len(certs_pool), self.rng.randint(0, 2))))
        card['funds'] = self.rng.sample(['公积金双边800', '公积金双边1200', '公积金双边2000',
                                          '公积金双边3000'], k=self.rng.randint(0, 2))
        card['insurance'] = self.rng.sample(
            ('城镇职工医保', '城镇职工养老', '商业医疗险', '重疾险', '意外险'),
            k=self.rng.randint(2, 4))

        # 家庭
        marital = self.rng.choices(
            ('已婚', '未婚', '离异', '丧偶'),
            weights=[55, 30, 10, 5], k=1)[0] if age > 22 else self.rng.choice(['未婚', '已婚', '已婚'])
        card['marital'] = marital
        kids = 0
        kids_ages = []
        if marital == '已婚' and age > 25:
            kids = self.rng.choices([0, 1, 2, 3], weights=[20, 45, 30, 5], k=1)[0]
            for _ in range(kids):
                ka = max(0, age - self.rng.choice([22, 24, 25, 26, 28, 30, 32]))
                kids_ages.append(ka)
        card['children_count'] = kids
        card['children_ages'] = sorted(kids_ages) if kids_ages else []
        card['elders_dependent'] = self.rng.choices([0, 1, 2, 3], weights=[40, 35, 20, 5], k=1)[0]
        card['household_type'] = D.derive_household_type(kids, card['elders_dependent'], marital)
        card['family_role'] = self.rng.choice(
            ('主要经济支柱', '共同经济支柱', '辅助经济来源'))

        # 住房（冗余字段填充）
        if card['housing_tenure'] == '租赁':
            card['housing_detail'] = '%s租房居住' % city

        # 心理
        card['emotion_status'] = marital
        stress_srcs = list(self.rng.sample(stress_pool, min(len(stress_pool),
                                                             self.rng.randint(1, len(stress_pool)))))
        if not stress_srcs:
            stress_srcs = ['工作与生活平衡', '收入增长焦虑']
        card['stress_sources'] = stress_srcs
        card['stress_level'] = self.rng.choices(['高', '中', '低'], weights=[25, 50, 25], k=1)[0]
        biases_opts = ['损失厌恶', '过度自信', '锚定效应', '羊群效应', '心理账户',
                       '禀赋效应', '现状偏见', '确认偏误', '沉没成本', '即时满足',
                       '风险厌恶', '风险寻求']
        card['biases'] = list(self.rng.sample(biases_opts, k=self.rng.randint(1, 3)))

        # 目标
        goals, opps, dream, _ = D.derive_goals(age, income)
        card['goals_short'] = goals
        card['opportunities'] = opps or ['行业经验积累', '职业技能提升']
        card['dream_cost'] = dream

        # 出行
        if income > 15000 and self.rng.random() < 0.7:
            card['transport_owned'] = self.rng.choice(['经济型', '中高端'])
            card['transport_mode'] = '自驾'
        elif income > 8000 and self.rng.random() < 0.4:
            card['transport_owned'] = '电动车摩托车'
            card['transport_mode'] = '两轮车'
        else:
            card['transport_owned'] = '公共交通为主'
            card['transport_mode'] = '公共交通'

        # provenance
        card['_legacy_ids'] = [card_id]
        card['_completeness'] = round(self.rng.uniform(0.85, 0.97), 3)
        card['_grounded'] = round(self.rng.uniform(0.40, 0.65), 3)
        n_missing = self.rng.randint(2, min(8, len(['birth_province','birth_city','housing_city','health_risks','certs','funds','biases'])))
        card['_sources'] = {
            'explicit': 28 + self.rng.randint(-5, 5),
            'derived': 20 + self.rng.randint(-3, 5),
            'assigned': 5 + self.rng.randint(0, 3),
            'missing': n_missing,
        }
        card['_missing'] = list(self.rng.sample(
            ['birth_province', 'birth_city', 'housing_city', 'health_risks',
             'certs', 'funds', 'biases'], k=n_missing))
        card['_raw'] = {'src_line': 'auto-gen|%s|%s岁|%s|%s,%d' % (
            name, age, occ_name, city, income)}

        return card, l1, l2

    def render_body(self, card, l1, l2):
        l2n = l2i.get(l2, (None, ''))[1]
        l3 = card['industry_l3']
        l3n = '职业族'
        pi_l1n = L1_NAMES.get(l1, '')
        out = []
        out.append('# %s · %s · %s 岁 · %s' % (
            card['name'], card['gender'], card['age'], card['occupation']))
        out.append('')
        out.append('> **一句话画像**：%s%s · %s。' % (
            card['housing_city'],
            '（%s）' % card['housing_tenure'] if card['housing_tenure'] != '未知' else '',
            '、'.join(card['stress_sources'][:2]) if card['stress_sources'] else '职业与家庭压力'))
        out.append('')
        out.append('| 项 | 值 |')
        out.append('|---|---|')
        out.append('| 行业域 | `%s` %s |' % (l1, pi_l1n))
        out.append('| 行业细分 | `%s` %s |' % (l2, l2n))
        out.append('| 职业族 | `%s` %s |' % (l3, l3n))
        out.append('| 原始职业 | %s |' % card['occupation'])
        out.append('| 月收入 | %s |' % _money(card['income_monthly']))
        out.append('| 收入结构 | %s（稳定性：%s） |' % (card['income_structure'], card['income_stability']))
        out.append('| 健康档 | %s |' % (card['health_grade'] or '未知'))
        out.append('| 完整度 | %.0f%% ｜ 有据率 %.0f%% |' % (
            card['_completeness'] * 100, card['_grounded'] * 100))
        out.append('')

        out.append('## 1. 基础档案\n')
        out.append('- **年龄**：%s 岁（%s）' % (card['age'], card['age_band']))
        out.append('- **性别**：%s' % card['gender'])
        out.append('- **出生**：%s 年（%s）' % (card['birth_year'], card['generation']))
        out.append('- **籍贯**：%s' % (card['birth_province'] or '未采集'))
        out.append('- **学历**：%s' % (card['education'] or '未采集'))
        out.append('- **现居**：%s ｜ %s' % (card['housing_city'], card['housing_detail']))
        out.append('- **身体**：%s' % ('、'.join(card['health_conditions']) or '未采集'))
        out.append('')

        out.append('## 2. 家庭与抚养\n')
        out.append('- **婚姻**：%s ｜ **情感状况**：%s' % (card['marital'], card['emotion_status']))
        out.append('- **子女**：%d 人%s' % (
            card['children_count'],
            ('（年龄 %s）' % '、'.join(str(a) for a in card['children_ages'])) if card['children_ages'] else ''))
        out.append('- **需赡养老人**：%d 人' % card['elders_dependent'])
        out.append('- **家庭类型**：%s ｜ **家庭角色**：%s' % (card['household_type'], card['family_role']))
        out.append('')

        out.append('## 3. 职业与收入\n')
        out.append('- **就业形态**：%s ｜ **用人单位**：%s ｜ **职业阶段**：%s' % (
            card['employment'], card['employer'], card['career_stage']))
        out.append('- **月收入**：%s（区间 %s）｜ **家庭月收入**：%s' % (
            _money(card['income_monthly']),
            '%s–%s' % (_money(card['income_range'][0]), _money(card['income_range'][1])),
            _money(card['household_monthly'])))
        out.append('- **工作强度**：每周约 %s 小时 ｜ 加班 %s ｜ 职业风险 %s' % (
            card['work_intensity']['weekly_hours'], card['work_intensity']['overtime'],
            card['work_intensity']['risk']))
        out.append('')

        out.append('## 4. 财务快照\n')
        out.append('- **月支出**：%s ｜ **月结余**：%s ｜ **储蓄率**：%.0f%%' % (
            _money(card['monthly_expense']),
            _money(card['income_monthly'] - card['monthly_expense']),
            card['savings_rate'] * 100))
        out.append('- **储蓄存量**：%s ｜ **负债存量**：%s ｜ **净结余**：%s' % (
            _money(card['savings_stock']), _money(card['debt_stock']), _money(card['net_worth'])))
        out.append('- **房贷余额**：%s' % (_money(card['mortgage_left']) if card['mortgage_left'] else '无'))
        if card['debts']:
            out.append('- **负债明细**：')
            for d in card['debts']:
                out.append('  - %s：%s（来源：%s）' % (d['type'], _money(d['balance']), d['source']))
        if card['assets']:
            out.append('- **资产明细**：')
            for a in card['assets'][:6]:
                out.append('  - %s：%s' % (a['type'], a.get('desc') or _money(a.get('value_cny'))))
        out.append('- **学历/证书**：%s ｜ **公积金/基金**：%s ｜ **保障**：%s' % (
            '、'.join(card['certs'][:6]) or '未采集',
            '、'.join(card['funds']) or '无',
            '、'.join(card['insurance']) or '未采集'))
        out.append('')

        out.append('## 5. 健康与压力\n')
        out.append('- **健康档位**：%s（A 稳定 / B 可控慢病或劳损 / C 重大风险）' % (card['health_grade'] or '未知'))
        out.append('- **健康状况**：%s' % ('、'.join(card['health_conditions']) or '未采集'))
        out.append('- **健康风险**：%s' % ('、'.join(card['health_risks']) or '未采集'))
        out.append('- **压力等级**：%s' % card['stress_level'])
        out.append('- **压力源**：%s' % ('、'.join(card['stress_sources']) or '未采集'))
        out.append('')

        out.append('## 6. 情感与人格\n')
        out.append('- **情感状况**：%s' % card['emotion_status'])
        out.append('- **行为金融偏差**：%s' % ('、'.join(card['biases']) or '未采集'))
        out.append('- **人格特征**：【待富化】')
        out.append('')

        out.append('## 7. 人生目标与机会\n')
        if card['goals_short']:
            for g in card['goals_short']:
                out.append('- **%s**：%s' % (g.get('horizon'), g.get('text')))
        if card['dream_cost']:
            out.append('- **梦想成本估算**：约 %s' % _money(card['dream_cost']))
        if card['opportunities']:
            out.append('- **机会/应对**：%s' % '；'.join(card['opportunities']))
        out.append('')

        out.append('## 8. 开局钩子\n')
        out.append('【待富化】')
        out.append('')

        out.append('## 9. 数据溯源\n')
        sc = card['_sources']
        out.append('- **旧编号**：%s ｜ **来源档案**：`%s`' % (card['id'], card['source_file']))
        out.append('- **字段来源统计**：有据 %d ｜ 推导 %d ｜ 分配（合成） %d ｜ 缺失 %d（共 59 字段）'
                    % (sc['explicit'], sc['derived'], sc['assigned'], sc['missing']))
        if card['_missing']:
            out.append('- **缺失字段**：%s' % '、'.join(card['_missing']))
        out.append('- **原档行**：`%s`' % card['_raw']['src_line'].replace('|', '\\|'))
        out.append('')
        return '\n'.join(out)

    def write_card(self, card, l1, l2):
        """渲染并写入卡片文件"""
        l1n = L1_NAMES.get(l1, '')
        l2n = l2i.get(l2, (None, ''))[1]
        region = PROVINCES.get(card['birth_province'], 'XX-未知')
        if card['housing_city'] in ['香港', '澳门', '台北', '新加坡', '东京', '悉尼', '温哥华']:
            region = 'OV-海外'
        age_band = card['age_band']
        d = os.path.join(self.out_dir,
                         '%s-%s' % (l1, _seg(l1n)),
                         '%s-%s' % (l2, _seg(l2n)),
                         '%s-%s' % (card['industry_l3'], _seg(card['occupation'])),
                         _seg(region), _seg(age_band))
        os.makedirs(d, exist_ok=True)
        body = self.render_body(card, l1, l2)
        fm = {k: v for k, v in card.items() if not k.startswith('_')}
        prov = {
            '_legacy_ids': card['_legacy_ids'],
            '_completeness': card['_completeness'],
            '_grounded': card['_grounded'],
            '_sources': card['_sources'],
            '_missing': card['_missing'],
            '_raw': card['_raw'],
        }
        text = '---\n' + yaml.dump(fm, allow_unicode=True, sort_keys=False,
                                     default_flow_style=None, width=10000).rstrip() + '\n'
        text += yaml.dump(prov, allow_unicode=True, sort_keys=False,
                          default_flow_style=None, width=10000).rstrip() + '\n'
        text += '---\n\n' + body
        fn = '%s-%s.md' % (card['id'], re.sub(r'[/\\]', '_', card['name']))
        fp = os.path.join(d, fn)
        with open(fp, 'w', encoding='utf-8') as fh:
            fh.write(text)
        return fp

    def generate_domain(self, l1, count, seed=None):
        """为某个 L1 域生成 count 张卡片"""
        if seed is not None:
            self.rng = random.Random(seed)
        # 获取该 L1 的所有 L2
        l2s = [(code, name) for code, (l1k, name) in l2i.items() if l1k == l1]
        if not l2s:
            return 0
        written = 0
        for i in range(count):
            l2, l2n = self.rng.choice(l2s)
            pool = OCC_POOL.get(l2)
            if not pool:
                # 如果该 L2 没预定义职业池，用通用职业
                pool = [('技术员', '全职', 8000, 2500, [], ['久坐'], ['工作与生活平衡']),
                        ('销售专员', '全职', 7500, 3500, [], ['应酬'], ['业绩压力', '客户拓展']),
                        ('行政文员', '全职', 6000, 1500, [], ['久坐'], ['晋升瓶颈', '重复性工作'])]
            occ_info = self.rng.choice(pool)
            card, l1r, l2r = self.generate_one(l1, l2, occ_info)
            self.write_card(card, l1r, l2r)
            written += 1
        return written


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--out', required=True)
    ap.add_argument('--l1', required=True, help='逗号分隔的 L1 码, 如 E,I,S,B,F')
    ap.add_argument('--count', type=int, default=200)
    ap.add_argument('--start-id', type=int, default=9002251)
    ap.add_argument('--batch-tag', default='v4.1')
    ap.add_argument('--seed', type=int, default=None)
    args = ap.parse_args()

    gen = Generator(args.out, start_id=args.start_id, batch_tag=args.batch_tag)
    l1s = [x.strip() for x in args.l1.split(',')]
    per_domain = max(1, args.count // len(l1s))
    total = 0
    for l1 in l1s:
        n = gen.generate_domain(l1, per_domain, seed=args.seed)
        total += n
        print('[gen_batch] L1=%s 生成 %d 张卡片 (next_id=%d)' % (l1, n, gen.next_id))
    print('[gen_batch] 合计生成 %d 张卡片' % total)


if __name__ == '__main__':
    sys.exit(main())
